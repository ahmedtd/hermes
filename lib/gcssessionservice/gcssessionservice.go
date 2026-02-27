package gcssessionservice

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/adk/session"
)

type GCSSessionState struct {
	state map[string]any
}

var _ session.State = (*GCSSessionState)(nil)

func (s *GCSSessionState) Get(key string) (any, error) {
	v, ok := s.state[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *GCSSessionState) Set(key string, value any) error {
	s.state[key] = value
	return nil
}

func (s *GCSSessionState) All() iter.Seq2[string, any] {
	return maps.All(s.state)
}

type GCSSessionEvents struct {
	events []*session.Event
}

var _ session.Events = (*GCSSessionEvents)(nil)

func (g *GCSSessionEvents) All() iter.Seq[*session.Event] {
	return slices.Values(g.events)
}

func (g *GCSSessionEvents) Len() int {
	return len(g.events)
}

func (g *GCSSessionEvents) At(i int) *session.Event {
	return g.events[i]
}

type GCSSession struct {
	id             string
	appName        string
	userID         string
	state          *GCSSessionState
	events         *GCSSessionEvents
	lastUpdateTime time.Time
}

var _ session.Session = (*GCSSession)(nil)

func (s *GCSSession) ID() string {
	return s.id
}

func (s *GCSSession) AppName() string {
	return s.appName
}

func (s *GCSSession) UserID() string {
	return s.userID
}

func (s *GCSSession) State() session.State {
	return s.state
}

func (s *GCSSession) Events() session.Events {
	return s.events
}

func (s *GCSSession) LastUpdateTime() time.Time {
	return s.lastUpdateTime
}

// Schema:
//   - apps/$APP/state: Object that holds the serialized app state
//   - apps/$APP/users/$USER/state: Object that holds the serialized user state
//   - apps/$APP/users/$USER/sessions/$SESSION/object: Object that holds a serialized GCSSession.
//
// TODO: May eventually need to break up the object to serialize events
// separately.  As the history gets longer, it will cause more and more traffic
// to read and write the session.
//
// TODO: It seems like the session state can be entirely reconstructed from the
// list of events?  InMemorySession seems to use AppendEvent as the mechanism
// for actually updating the state.
type GCSSessionService struct {
	client *storage.Client
	bucket string
}

var _ session.Service = (*GCSSessionService)(nil)

func appStateKey(app string) string {
	return path.Join("apps", app, "state")
}

func userStateKey(app, user string) string {
	return path.Join("apps", app, "users", user, "state")
}

func sessionObjectKey(app, user, session string) string {
	return path.Join("apps", app, "users", user, "sessions", session, "object")
}

func (s *GCSSessionService) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required, got app_name: %q, user_id: %q", req.AppName, req.UserID)
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	// TODO: Check if session already exists

	appState := map[string]any{}
	for k, v := range req.State {
		if strings.HasPrefix(k, "app:") {
			appState[k] = v
		}
	}
	if err := s.writeJSON(ctx, appStateKey(req.AppName), appState); err != nil {
		return nil, fmt.Errorf("while writing app state: %w", err)
	}

	userState := map[string]any{}
	for k, v := range req.State {
		if strings.HasPrefix(k, "user:") {
			userState[k] = v
		}
	}
	if err := s.writeJSON(ctx, userStateKey(req.AppName, req.UserID), userState); err != nil {
		return nil, fmt.Errorf("while writing user state: %w", err)
	}

	concreteSession := &GCSSession{
		id:             sessionID,
		appName:        req.AppName,
		userID:         req.UserID,
		state:          &GCSSessionState{state: req.State},
		events:         &GCSSessionEvents{},
		lastUpdateTime: time.Now(),
	}

	// TODO: Should the session state contain the app and user state as well?
	// That seems to be what InMemoryState does, but it doesn't make sense?
	if err := s.writeJSON(ctx, sessionObjectKey(req.AppName, req.UserID, sessionID), concreteSession); err != nil {
		return nil, fmt.Errorf("while writing session state: %w", err)
	}

	return &session.CreateResponse{
		Session: concreteSession,
	}, nil

}

func (s *GCSSessionService) writeJSON(ctx context.Context, key string, content any) error {
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("while marshaling: %w", err)
	}

	w := s.client.Bucket(s.bucket).Object(key).NewWriter(ctx)
	if _, err := w.Write(contentBytes); err != nil {
		return fmt.Errorf("while writing content: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("while closing writer: %w", err)
	}
	return nil
}

func (s *GCSSessionService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	appName, userID, sessionID := req.AppName, req.UserID, req.SessionID
	if appName == "" || userID == "" || sessionID == "" {
		return nil, fmt.Errorf("app_name, user_id, session_id are required, got app_name: %q, user_id: %q, session_id: %q", appName, userID, sessionID)
	}
}

func (s *GCSSessionService) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
}

func (s *GCSSessionService) Delete(ctx context.Context, req *session.DeleteRequest) error {
}

func (s *GCSSessionService) AppendEvent(ctx context.Context, curSession session.Session, event *session.Event) error {
}

const (
	appPrefix  = "app:"
	userPrefix = "user:"
	tempPrefix = "temp:"
)

// ExtractStateDeltas splits a single state delta map into three separate maps
// for app, user, and session states based on key prefixes.
// Temporary keys (starting with TempStatePrefix) are ignored.
//
// Copied from ADK internal sessionutils
func ExtractStateDeltas(delta map[string]any) (
	appStateDelta, userStateDelta, sessionStateDelta map[string]any,
) {
	// Initialize the maps to be returned.
	appStateDelta = make(map[string]any)
	userStateDelta = make(map[string]any)
	sessionStateDelta = make(map[string]any)

	if delta == nil {
		return appStateDelta, userStateDelta, sessionStateDelta
	}

	for key, value := range delta {
		if cleanKey, found := strings.CutPrefix(key, appPrefix); found {
			appStateDelta[cleanKey] = value
		} else if cleanKey, found := strings.CutPrefix(key, userPrefix); found {
			userStateDelta[cleanKey] = value
		} else if !strings.HasPrefix(key, tempPrefix) {
			// This key belongs to the session state, as long as it's not temporary.
			sessionStateDelta[key] = value
		}
	}
	return appStateDelta, userStateDelta, sessionStateDelta
}

// MergeStates combines app, user, and session state maps into a single map
// for client-side responses, adding the appropriate prefixes back.
//
// Copied from ADK internal sessionutils
func MergeStates(appState, userState, sessionState map[string]any) map[string]any {
	// Pre-allocate map capacity for efficiency.
	totalSize := len(appState) + len(userState) + len(sessionState)
	mergedState := make(map[string]any, totalSize)

	maps.Copy(mergedState, sessionState)

	for key, value := range appState {
		mergedState[appPrefix+key] = value
	}

	for key, value := range userState {
		mergedState[userPrefix+key] = value
	}

	return mergedState
}
