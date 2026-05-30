package strategy

import "context"

// Starter is an optional extension of Strategy for implementations that own a
// side goroutine (e.g. a rate-polling loop). The engine calls Start(ctx) on any
// registered strategy that satisfies this interface before the engine begins
// processing updates. Implementations must respect ctx cancellation and exit
// cleanly when ctx.Done() is closed.
type Starter interface {
	Start(ctx context.Context) error
}
