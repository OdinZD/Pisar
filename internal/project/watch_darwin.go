package project

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsevents"
)

// fsEventsLatency is how long FSEvents coalesces events before delivering
// them. The Watcher debounces anyway, so this only has to be small enough not
// to add noticeably to the refresh delay, yet large enough to batch the burst
// of events a single save produces.
const fsEventsLatency = 50 * time.Millisecond

// fsEventsBuffer holds event batches between the FSEvents callback and the
// forwarding goroutine; the callback blocks when it is full.
const fsEventsBuffer = 64

// rescanFlags are the events after which the tree below may have changed in
// ways the stream does not list one by one.
const rescanFlags = fsevents.MustScanSubDirs | fsevents.RootChanged |
	fsevents.Mount | fsevents.Unmount |
	fsevents.KernelDropped | fsevents.UserDropped | fsevents.EventIDsWrapped

// FSEventsSource is the macOS Source.
type FSEventsSource struct{}

// Start watches root with an FSEvents stream until ctx is cancelled.
func (FSEventsSource) Start(ctx context.Context, root string) (<-chan []Change, error) {
	// FSEvents reports real paths, so watch the resolved root.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("project: resolve %q: %w", root, err)
	}
	es := &fsevents.EventStream{
		Paths:   []string{resolved},
		Flags:   fsevents.FileEvents | fsevents.WatchRoot,
		Latency: fsEventsLatency,
		Events:  make(chan []fsevents.Event, fsEventsBuffer),
	}
	if err := es.Start(); err != nil {
		return nil, fmt.Errorf("project: start FSEvents stream for %q: %w", resolved, err)
	}

	out := make(chan []Change, fsEventsBuffer)
	go func() {
		defer close(out)
		for {
			select {
			case batch := <-es.Events:
				select {
				case out <- convertEvents(batch):
				case <-ctx.Done(): // nobody reads out any more
				}
			case <-ctx.Done():
				// Keep draining while stopping: the FSEvents callback blocks on
				// a full Events channel, and Stop must not be held up by it.
				stopped := make(chan struct{})
				go func() {
					defer close(stopped)
					es.Stop()
				}()
				for {
					select {
					case <-es.Events:
					case <-stopped:
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func convertEvents(batch []fsevents.Event) []Change {
	out := make([]Change, len(batch))
	for i, e := range batch {
		out[i] = Change{
			Path:   e.Path,
			Dir:    e.Flags&fsevents.ItemIsDir != 0,
			Rescan: e.Flags&rescanFlags != 0,
		}
	}
	return out
}
