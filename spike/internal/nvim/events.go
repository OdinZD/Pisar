package nvim

// eventBuffer is the capacity of the Events channel. When a consumer does not
// read, emit drops the oldest event, so Neovim and the RPC loop never block
// and the newest events always survive.
const eventBuffer = 16

// emit queues ev without ever blocking.
func (p *Process) emit(ev Event) {
	p.evMu.Lock()
	defer p.evMu.Unlock()
	if p.evClosed {
		return
	}
	for {
		select {
		case p.events <- ev:
			return
		default:
		}
		// Full: make room by dropping the oldest. The consumer may take it
		// first, in which case the next send simply succeeds.
		select {
		case <-p.events:
		default:
		}
	}
}

// closeEvents closes the Events channel exactly once. It is called when
// Neovim announces it is leaving (after all its save events, because
// notifications on one channel are ordered and handled in order) and, as a
// fallback for a killed process, when the process has been reaped.
func (p *Process) closeEvents() {
	p.evMu.Lock()
	defer p.evMu.Unlock()
	if !p.evClosed {
		p.evClosed = true
		close(p.events)
	}
}
