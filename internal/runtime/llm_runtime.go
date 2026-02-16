package runtime

import (
	"context"
	"fmt"

	"github.com/dgriffin831/localclaw/internal/llm"
)

func (a *App) promptStreamFromClient(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, <-chan error) {
	client, ok := a.llm.(llm.RequestClient)
	if !ok || !a.llm.Capabilities().SupportsRequestOptions {
		events := make(chan llm.StreamEvent)
		errs := make(chan error, 1)
		close(events)
		errs <- fmt.Errorf("llm client does not support request-based prompt streaming")
		close(errs)
		return events, errs
	}
	return client.PromptStreamRequest(ctx, req)
}
