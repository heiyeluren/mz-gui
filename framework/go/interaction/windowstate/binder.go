package windowstate

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

var (
	ErrInvalidBinder = errors.New("页面恢复绑定无效")
	ErrMissingRoute  = errors.New("页面恢复路由未注册")
)

type PageCodec struct {
	RouteID string
	Save    func(windowID string) ([]byte, error)
	Restore func(windowID string, payload []byte) error
}

type Binder struct {
	store  PageStore
	mu     sync.Mutex
	codecs map[string]PageCodec
}

func NewBinder(store PageStore) (*Binder, error) {
	if store == nil {
		return nil, ErrInvalidBinder
	}
	return &Binder{store: store, codecs: make(map[string]PageCodec)}, nil
}

func (binder *Binder) Register(codec PageCodec) error {
	if binder == nil {
		return ErrInvalidBinder
	}
	routeID := strings.TrimSpace(codec.RouteID)
	if routeID == "" || codec.Save == nil || codec.Restore == nil {
		return ErrInvalidBinder
	}
	binder.mu.Lock()
	defer binder.mu.Unlock()
	if _, exists := binder.codecs[routeID]; exists {
		return ErrInvalidBinder
	}
	codec.RouteID = routeID
	binder.codecs[routeID] = codec
	return nil
}

func (binder *Binder) SaveAll(ctx context.Context, windowID string) error {
	if binder == nil || ctx == nil || strings.TrimSpace(windowID) == "" {
		return ErrInvalidBinder
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	binder.mu.Lock()
	codecs := make([]PageCodec, 0, len(binder.codecs))
	for _, codec := range binder.codecs {
		codecs = append(codecs, codec)
	}
	binder.mu.Unlock()
	for _, codec := range codecs {
		payload, err := codec.Save(windowID)
		if err != nil {
			return err
		}
		revision := uint64(1)
		if existing, err := binder.store.LoadPage(ctx, windowID, codec.RouteID); err == nil {
			revision = existing.Revision + 1
		}
		if err := binder.store.SavePage(ctx, Snapshot{
			WindowID: windowID, RouteID: codec.RouteID, Revision: revision, Payload: payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (binder *Binder) Routes() []string {
	if binder == nil {
		return nil
	}
	binder.mu.Lock()
	defer binder.mu.Unlock()
	routes := make([]string, 0, len(binder.codecs))
	for routeID := range binder.codecs {
		routes = append(routes, routeID)
	}
	sort.Strings(routes)
	return routes
}

func (binder *Binder) Require(routeIDs ...string) error {
	if binder == nil {
		return ErrInvalidBinder
	}
	binder.mu.Lock()
	defer binder.mu.Unlock()
	missing := make([]string, 0)
	for _, routeID := range routeIDs {
		routeID = strings.TrimSpace(routeID)
		if routeID == "" {
			return ErrInvalidBinder
		}
		if _, ok := binder.codecs[routeID]; !ok {
			missing = append(missing, routeID)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s", ErrMissingRoute, strings.Join(missing, ","))
	}
	return nil
}

func (binder *Binder) RestoreAll(ctx context.Context, windowID string) error {
	_, err := binder.RestoreRoutes(ctx, windowID)
	return err
}

func (binder *Binder) RestoreRoutes(ctx context.Context, windowID string) ([]string, error) {
	if binder == nil || ctx == nil || strings.TrimSpace(windowID) == "" {
		return nil, ErrInvalidBinder
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binder.mu.Lock()
	codecs := make([]PageCodec, 0, len(binder.codecs))
	for _, codec := range binder.codecs {
		codecs = append(codecs, codec)
	}
	binder.mu.Unlock()
	sort.Slice(codecs, func(i, j int) bool { return codecs[i].RouteID < codecs[j].RouteID })
	restored := make([]string, 0)
	for _, codec := range codecs {
		snapshot, err := binder.store.LoadPage(ctx, windowID, codec.RouteID)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := codec.Restore(windowID, snapshot.Payload); err != nil {
			return nil, err
		}
		restored = append(restored, codec.RouteID)
	}
	return restored, nil
}
