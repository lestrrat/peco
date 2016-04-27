package peco

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"

	"github.com/jessevdk/go-flags"
	"github.com/peco/peco/internal/sighandler"
	"github.com/peco/peco/internal/util"
)

var ErrSignalReceived = errors.New("received signal")

func showHelp() {
	// The ONLY reason we're not using go-flags' help option is
	// because I wanted to tweak the format just a bit... but
	// there wasn't an easy way to do so
	os.Stderr.WriteString(`
Usage: peco [options] [FILE]

Options:
`)

	t := reflect.TypeOf(CLIOptions{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag

		var o string
		if s := tag.Get("short"); s != "" {
			o = fmt.Sprintf("-%s, --%s", tag.Get("short"), tag.Get("long"))
		} else {
			o = fmt.Sprintf("--%s", tag.Get("long"))
		}

		fmt.Fprintf(
			os.Stderr,
			"  %-21s %s\n",
			o,
			tag.Get("description"),
		)
	}
}

// BufferSize returns the specified buffer size. Fulfills CtxOptions
func (o CLIOptions) BufferSize() int {
	return o.OptBufferSize
}

// EnableNullSep returns true if --null was specified. Fulfills CtxOptions
func (o CLIOptions) EnableNullSep() bool {
	return o.OptEnableNullSep
}

func (o CLIOptions) InitialIndex() int {
	return o.OptInitialIndex
}

func (o CLIOptions) LayoutType() string {
	return o.OptLayout
}

func (cli *CLI) parseOptions() (*CLIOptions, []string, error) {
	opts := &CLIOptions{}
	p := flags.NewParser(opts, flags.PrintErrors)
	args, err := p.Parse()
	if err != nil {
		showHelp()
		return nil, nil, err
	}

	if opts.OptLayout != "" {
		if !IsValidLayoutType(LayoutType(opts.OptLayout)) {
			return nil, nil, fmt.Errorf("unknown layout: '%s'\n", opts.OptLayout)
		}
	}

	return opts, args, nil
}

func (cli *CLI) Run() error {
	opts, args, err := cli.parseOptions()
	if err != nil {
		return err
	}

	if opts.OptHelp {
		showHelp()
		return nil
	}

	if opts.OptVersion {
		fmt.Fprintf(os.Stderr, "peco: %s\n", version)
		return nil
	}

	var in *os.File

	// receive in from either a file or Stdin
	switch {
	case len(args) > 0:
		in, err = os.Open(args[0])
		if err != nil {
			return err
		}
	case !util.IsTty(os.Stdin.Fd()):
		in = os.Stdin
	default:
		return fmt.Errorf("error: You must supply something to work with via filename or stdin")
	}

	ctx := NewCtx(opts)
	defer func() {
		ch := ctx.ResultCh()
		if ch == nil {
			return
		}

		for match := range ch {
			line := match.Output()
			if line[len(line)-1] != '\n' {
				line = line + "\n"
			}
			fmt.Fprint(os.Stdout, line)
		}
	}()

	if opts.OptRcfile == "" {
		file, err := LocateRcfile()
		if err == nil {
			opts.OptRcfile = file
		}
	}

	// Default matcher is IgnoreCase
	ctx.SetCurrentFilterByName(IgnoreCaseMatch)

	if opts.OptRcfile != "" {
		err = ctx.ReadConfig(opts.OptRcfile)
		if err != nil {
			return err
		}
	}

	if len(opts.OptPrompt) > 0 {
		ctx.SetPrompt(opts.OptPrompt)
	}

	initialFilter := ""
	if len(opts.OptInitialFilter) <= 0 && len(opts.OptInitialMatcher) > 0 {
		initialFilter = opts.OptInitialMatcher
	} else if len(opts.OptInitialFilter) > 0 {
		initialFilter = opts.OptInitialFilter
	}
	if initialFilter != "" {
		if err := ctx.SetCurrentFilterByName(initialFilter); err != nil {
			return fmt.Errorf("unknown matcher: '%s'\n", initialFilter)
		}
	}

	// Try waiting for something available in the source stream
	// before doing any terminal initialization (also done by termbox)
	reader := ctx.NewBufferReader(in)
	ctx.AddWaitGroup(1)
	go func() {
		reader.Loop()
		if opts.OptSelect1 && reader.GetRawLineBufferSize() == 1 {
			if l, err := reader.GetCurrentLineBuffer().LineAt(0); err == nil {
				ctx.resultCh = make(chan Line)
				ctx.ExitWith(nil)
				ctx.resultCh <- l
				close(ctx.resultCh)
			}
		}
	}()

	// This channel blocks until we receive something from `in`
	<-reader.InputReadyCh()

	if err := util.TtyReady(); err != nil {
		return err
	}
	defer util.TtyTerm()

	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Close()

	ctx.startInput()
	view := ctx.NewView()
	filter := ctx.NewFilter()

	sig := sighandler.New(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	sig.EndFunc = ctx.ReleaseWaitGroup
	sig.SignalReceivedFunc = func(_ os.Signal) bool {
		ctx.ExitWith(ErrSignalReceived)
		return false
	}
	go sig.Loop(ctx.LoopCh())

	loopers := []interface {
		Loop()
	}{
		view,
		filter,
	}
	for _, looper := range loopers {
		ctx.AddWaitGroup(1)
		go looper.Loop()
	}

	if len(opts.OptQuery) > 0 {
		ctx.SetQuery([]rune(opts.OptQuery))
		ctx.ExecQuery()
	} else {
		ctx.SendDraw(false)
	}

	ctx.WaitDone()

	return ctx.Error()
}
