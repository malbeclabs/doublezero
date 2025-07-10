package netns

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vishvananda/netns"
)

func WaitForNamespace(log *slog.Logger, namespace string, timeout time.Duration) (*netns.NsHandle, error) {
	waitCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			return nil, fmt.Errorf("timed out waiting for namespace %q to be created", namespace)
		default:
			ns, err := netns.GetFromName(namespace)
			if err == nil {
				return &ns, nil
			}
			log.Info("Waiting for namespace to be created", "namespace", namespace)
			time.Sleep(1 * time.Second)
		}
	}
}
