package gcssessionservice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"google.golang.org/adk/session"
	"google.golang.org/api/iterator"
)

type GCSSessionState struct {
	State map[string]any `json:"state"`
}

var _ session.State = (*GCSSessionState)(nil)

func (s *GCSSessionState) Get(key string) (any, error) {
	v, ok := s.State[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *GCSSessionState) Set(key string, value any) error {
	if s.State == nil {
		s.State = map[string]any{}
	}
	s.State[key] = value
	return nil
}

func (s *GCSSessionState) All() iter.Seq2[string, any] {
	return maps.All(s.State)
}

type GCSSessionEvents struct {
	Events []*session.Event `json:"events"`
}

var _ session.Events = (*GCSSessionEvents)(nil)

func (g *GCSSessionEvents) All() iter.Seq[*session.Event] {
	return slices.Values(g.Events)
}

func (g *GCSSessionEvents) Len() int {
	return len(g.Events)
}

func (g *GCSSessionEvents) At(i int) *session.Event {
	return g.Events[i]
}

type GCSSession struct {
	SessionIDVal      string            `json:"sessionID"`
	AppNameVal        string            `json:"appName"`
	UserIDVal         string            `json:"userID"`
	StateVal          *GCSSessionState  `json:"state"`
	EventsVal         *GCSSessionEvents `json:"events"`
	LastUpdateTimeVal time.Time         `json:"time"`
}

var _ session.Session = (*GCSSession)(nil)

func (s *GCSSession) ID() string {
	return s.SessionIDVal
}

func (s *GCSSession) AppName() string {
	return s.AppNameVal
}

func (s *GCSSession) UserID() string {
	return s.UserIDVal
}

func (s *GCSSession) State() session.State {
	return s.StateVal
}

func (s *GCSSession) Events() session.Events {
	return s.EventsVal
}

func (s *GCSSession) LastUpdateTime() time.Time {
	return s.LastUpdateTimeVal
}

// Schema:
//   - apps/$APP/state: Object that holds the serialized app state
//   - apps/$APP/users/$USER/state: Object that holds the serialized user state
//   - apps/$APP/users/$USER/sessions/$SESSION/object: Object that holds a serialized GCSSession.
//   - apps/$APP/users/$USER/sessions/$SESSION/events/$EVENT
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

func New(client *storage.Client, bucket string) *GCSSessionService {
	slog.Debug("Initializing GCS Session Service", slog.String("bucket", bucket))
	return &GCSSessionService{
		client: client,
		bucket: bucket,
	}
}

var _ session.Service = (*GCSSessionService)(nil)

func appStateKey(app string) string {
	return path.Join("apps", app, "state")
}

func userStateKey(app, user string) string {
	return path.Join("apps", app, "users", user, "state")
}

func sessionPrefixKey(app, user string) string {
	return path.Join("apps", app, "users", user, "sessions") + "/"
}

func sessionObjectKey(app, user, session string) string {
	return path.Join("apps", app, "users", user, "sessions", session, "object")
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

func (s *GCSSessionService) readJSON(ctx context.Context, key string, dest any) error {
	r, err := s.client.Bucket(s.bucket).Object(key).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("while opening reader: %w", err)
	}

	sessionBytes, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("while reading bytes: %w", err)
	}

	if err := r.Close(); err != nil {
		return fmt.Errorf("while closing reader: %w", err)
	}

	if err := json.Unmarshal(sessionBytes, dest); err != nil {
		return fmt.Errorf("while unmarshaling session: %w", err)
	}

	slog.DebugContext(ctx, "gcssessionservice read JSON", slog.Any("val", dest))

	return nil
}

func (s *GCSSessionService) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	slog.DebugContext(ctx, "gcssessionservice.Create", slog.Any("req", req))

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
		SessionIDVal:      sessionID,
		AppNameVal:        req.AppName,
		UserIDVal:         req.UserID,
		StateVal:          &GCSSessionState{State: req.State},
		EventsVal:         &GCSSessionEvents{},
		LastUpdateTimeVal: time.Now(),
	}

	// TODO: Should the session state contain the app and user state as well?
	// That seems to be what InMemoryState does, but it doesn't make sense?
	if err := s.writeJSON(ctx, sessionObjectKey(req.AppName, req.UserID, sessionID), concreteSession); err != nil {
		return nil, fmt.Errorf("while writing session state: %w", err)
	}

	slog.InfoContext(ctx, "Made session", slog.Any("session", concreteSession))

	return &session.CreateResponse{
		Session: concreteSession,
	}, nil

}

func (s *GCSSessionService) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	slog.DebugContext(ctx, "gcssessionservice.Get", slog.Any("req", req))

	var sessionObj *GCSSession
	err := s.readJSON(ctx, sessionObjectKey(req.AppName, req.UserID, req.SessionID), &sessionObj)
	if err != nil {
		return nil, fmt.Errorf("while loading session from GCS: %w", err)
	}

	// TODO: Overwrite app and user state in the session?  Not clear what
	// semantics ADK expects.

	return &session.GetResponse{
		Session: sessionObj,
	}, nil
}

func (s *GCSSessionService) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	slog.DebugContext(ctx, "gcssessionservice.List", slog.Any("req", req))

	it := s.client.Bucket(s.bucket).Objects(ctx, &storage.Query{
		Delimiter: "/",
		Prefix:    sessionPrefixKey(req.AppName, req.UserID),
	})

	sessions := []session.Session{}

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		} else if err != nil {
			return nil, fmt.Errorf("while iterating over objects: %w", err)
		}

		var sessionObj *GCSSession
		err = s.readJSON(ctx, attrs.Prefix+"object", &sessionObj)
		if err != nil {
			return nil, fmt.Errorf("while loading session from GCS: %w", err)
		}

		// TODO: Overwrite app and user state in the session?  Not clear what
		// semantics ADK expects.

		sessions = append(sessions, sessionObj)
	}

	return &session.ListResponse{
		Sessions: sessions,
	}, nil
}

func (s *GCSSessionService) Delete(ctx context.Context, req *session.DeleteRequest) error {
	slog.DebugContext(ctx, "gcssessionservice.Delete", slog.Any("req", req))

	key := sessionObjectKey(req.AppName, req.UserID, req.SessionID)
	if err := s.client.Bucket(s.bucket).Object(key).Delete(ctx); err != nil {
		return fmt.Errorf("while deleting object: %w", err)
	}
	return nil
}

func (s *GCSSessionService) AppendEvent(ctx context.Context, curSessionAny session.Session, event *session.Event) error {
	curSession, ok := curSessionAny.(*GCSSession)
	if !ok {
		return fmt.Errorf("wrong session type %T", curSessionAny)
	}

	slog.DebugContext(ctx, "gcssessionservice.AppendEvent", slog.String("session-id", curSession.ID()), slog.Any("event", event))

	if event.Partial {
		return nil
	}

	for k, v := range event.Actions.StateDelta {
		if err := curSession.StateVal.Set(k, v); err != nil {
			return fmt.Errorf("while updating session state: %w", err)
		}
	}

	// TODO: Drop any keys with prefix temp: from the event before recording it
	// into the session.  InMemorySession does this, but I'm not really sure of
	// the implications.  We just put them in the session state.  I guess if we
	// reload from a snapshot, we want the temp keys to be gone?  Seems fishy.

	curSession.EventsVal.Events = append(curSession.EventsVal.Events, event)
	curSession.LastUpdateTimeVal = event.Timestamp

	if err := s.writeJSON(ctx, sessionObjectKey(curSession.AppNameVal, curSession.UserIDVal, curSession.SessionIDVal), curSession); err != nil {
		return fmt.Errorf("while saving updated session to GCS: %w", err)
	}

	return nil
}
