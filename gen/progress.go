package gen

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Progress reports what a run is doing while it does it.
//
// It writes to stderr, because stdout is the response protocol: a protoc plugin
// that prints anything to stdout has corrupted its own output. buf shows a
// plugin's stderr, so this is what reaches the person running the build.
//
// It writes whole lines and never a carriage return. Under buf, stderr is a
// pipe rather than a terminal, so a spinner that overwrites one line leaves the
// log full of \r and unreadable. The percentage goes inside the line instead.
type Progress struct {
	w     io.Writer
	start time.Time

	mu       sync.Mutex
	targets  int
	done     int
	files    int
	targetAt time.Time
}

// NewProgress returns a reporter writing to w. A nil writer reports nothing, so
// callers do not have to branch.
func NewProgress(w io.Writer) *Progress {
	if w == nil {
		w = io.Discard
	}
	return &Progress{w: w, start: time.Now()}
}

// Start announces the run.
func (p *Progress) Start(entities, targets int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.targets = targets
	p.start = time.Now()
	fmt.Fprintf(p.w, "calque: %s, %s\n", plural(entities, "entity", "entities"), plural(targets, "target", "targets"))
}

// TargetStart notes which target is running, so a slow one is identifiable even
// if the run never finishes.
func (p *Progress) TargetStart(label, backend string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.targetAt = time.Now()
}

// TargetDone reports one target's result and the share of the run completed.
func (p *Progress) TargetDone(label, backend string, files int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.done++
	p.files += files

	pct := 100
	if p.targets > 0 {
		pct = p.done * 100 / p.targets
	}
	fmt.Fprintf(p.w, "calque: [%d/%d] %3d%%  %s → %s  %s  (%s)\n",
		p.done, p.targets, pct, label, backend,
		plural(files, "file", "files"), took(time.Since(p.targetAt)))
}

// Step reports movement inside a target, for one with enough entities that the
// target-level line is not enough. A target that never calls it still gets its
// own line.
func (p *Progress) Step(label string, done, total int, what string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	pct := 100
	if total > 0 {
		pct = done * 100 / total
	}
	fmt.Fprintf(p.w, "calque:   %s %3d%%  (%d/%d) %s\n", label, pct, done, total, what)
}

// Finish reports the whole run.
func (p *Progress) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, "calque: %s, %s\n", plural(p.files, "file", "files"), took(time.Since(p.start)))
}

// took renders a duration the way a build log wants it: no more precision than
// the number deserves.
func took(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
