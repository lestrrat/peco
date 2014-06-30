package peco

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

type Selection []int

func (s Selection) Has(v int) bool {
	for _, i := range []int(s) {
		if i == v {
			return true
		}
	}
	return false
}

func (s *Selection) Add(v int) {
	if s.Has(v) {
		return
	}
	*s = Selection(append([]int(*s), v))
	sort.Sort(s)
}

func (s *Selection) Remove(v int) {
	a := []int(*s)
	for k, i := range a {
		if i == v {
			tmp := a[:k]
			tmp = append(tmp, a[k+1:]...)
			*s = Selection(tmp)
			return
		}
	}
}

func (s *Selection) Clear() {
	*s = Selection([]int{})
}

func (s Selection) Len() int {
	return len(s)
}

func (s Selection) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Selection) Less(i, j int) bool {
	return s[i] < s[j]
}

// Ctx contains all the important data. while you can easily access
// data in this struct from anwyehre, only do so via channels
type Ctx struct {
	enableSep      bool
	result         []Match
	loopCh         chan struct{}
	queryCh        chan string
	drawCh         chan []Match
	statusMsgCh    chan string
	pagingCh       chan PagingRequest
	mutex          sync.Mutex
	query          []rune
	caretPos       int
	currentLine    int
	selection      Selection
	lines          []Match
	current        []Match
	config         *Config
	Matchers       []Matcher
	CurrentMatcher int
	ExitStatus     int

	wait *sync.WaitGroup
}

func NewCtx(enableSep bool) *Ctx {
	return &Ctx{
		enableSep,
		[]Match{},
		make(chan struct{}),         // loopCh. You never send messages to this. no point in buffering
		make(chan string, 5),        // queryCh.
		make(chan []Match, 5),       // drawCh.
		make(chan string, 5),				 // statusMsgCh
		make(chan PagingRequest, 5), // pagingCh
		sync.Mutex{},
		[]rune{},
		0,
		1,
		Selection([]int{}),
		[]Match{},
		nil,
		NewConfig(),
		[]Matcher{
			NewIgnoreCaseMatcher(enableSep),
			NewCaseSensitiveMatcher(enableSep),
			NewRegexpMatcher(enableSep),
		},
		0,
		0,
		&sync.WaitGroup{},
	}
}

func (c *Ctx) ReadConfig(file string) error {
	if err := c.config.ReadFilename(file); err != nil {
		return err
	}

	if err := c.LoadCustomMatcher(); err != nil {
		return err
	}
	c.SetCurrentMatcher(c.config.Matcher)

	return nil
}

func (c *Ctx) Result() []Match {
	return c.result
}

func (c *Ctx) AddWaitGroup(v int) {
	c.wait.Add(v)
}

func (c *Ctx) ReleaseWaitGroup() {
	c.wait.Done()
}

func (c *Ctx) WaitDone() {
	c.wait.Wait()
}

func (c *Ctx) LoopCh() chan struct{} {
	return c.loopCh
}

func (c *Ctx) QueryCh() chan string {
	return c.queryCh
}

func (c *Ctx) DrawCh() chan []Match {
	return c.drawCh
}

func (c *Ctx) StatusMsgCh() chan string {
	return c.statusMsgCh
}

func (c *Ctx) PagingCh() chan PagingRequest {
	return c.pagingCh
}

func (c *Ctx) Terminate() {
	close(c.loopCh)
}

func (c *Ctx) ExecQuery(v string) {
	c.queryCh <- v
}

func (c *Ctx) DrawMatches(m []Match) {
	c.drawCh <- m
}
func (c *Ctx) Refresh() {
	c.DrawMatches(nil)
}

func (c *Ctx) Buffer() []Match {
	// Copy lines so it's safe to read it
	lcopy := make([]Match, len(c.lines))
	copy(lcopy, c.lines)
	return lcopy
}

func (c *Ctx) ReadBuffer(input io.Reader) error {
	go func(scanner *bufio.Scanner) {
		loop: for {
			select {
			case <-c.LoopCh():
				return
			default:
				if !scanner.Scan() {
					break loop
				}
				line := scanner.Text()
				c.lines = append(c.lines, NewNoMatch(line, c.enableSep))
			}
		}
	}(bufio.NewScanner(input))

	return nil
}

func (c *Ctx) NewView() *View {
	return &View{c}
}

func (c *Ctx) NewFilter() *Filter {
	return &Filter{c, make(chan string)}
}

func (c *Ctx) NewInput() *Input {
	return &Input{c, &sync.Mutex{}}
}

func (c *Ctx) Stop() {
	close(c.LoopCh())
}

func (c *Ctx) SetQuery(q []rune) {
	c.query = q
	c.caretPos = len(q)
}

func (c *Ctx) Matcher() Matcher {
	return c.Matchers[c.CurrentMatcher]
}

func (c *Ctx) AddMatcher(m Matcher) error {
	if err := m.Verify(); err != nil {
		return fmt.Errorf("Verification for custom matcher failed: %s", err)
	}
	c.Matchers = append(c.Matchers, m)
	return nil
}

func (c *Ctx) SetCurrentMatcher(n string) bool {
	for i, m := range c.Matchers {
		if m.String() == n {
			c.CurrentMatcher = i
			return true
		}
	}
	return false
}

func (c *Ctx) LoadCustomMatcher() error {
	if len(c.config.CustomMatcher) == 0 {
		return nil
	}

	for name, args := range c.config.CustomMatcher {
		if err := c.AddMatcher(NewCustomMatcher(c.enableSep, name, args)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Ctx) ExitWith(i int) {
	c.ExitStatus = i
	c.Stop()
}

func (c *Ctx) SignalHandlerLoop(sigCh chan os.Signal) {
	defer c.ReleaseWaitGroup()

	for {
		select {
		case <-c.LoopCh():
			return
		case <-sigCh:
			// XXX For future reference: DO NOT, and I mean DO NOT call
			// termbox.Close() here. Calling termbox.Close() twice in our
			// context actually BLOCKS. Can you believe it? IT BLOCKS.
			//
			// So if we called termbox.Close() here, and then in main()
			// defer termbox.Close() blocks. Not cool.
			c.ExitWith(1)
			return
		}
	}
}
