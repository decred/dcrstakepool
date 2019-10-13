package system

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/gob"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// SQLStore stores gorilla sessions in a database.
type SQLStore struct {
	Options *sessions.Options
	codecs  []securecookie.Codec
	dbMap   *gorp.DbMap
}

// NewSQLStore returns a new SQLStore. The keyPairs are used in the same way as
// the gorilla sessions CookieStore.
func NewSQLStore(ctx context.Context, wg *sync.WaitGroup, dbMap *gorp.DbMap, keyPairs ...[]byte) *SQLStore {
	s := &SQLStore{
		codecs: securecookie.CodecsFromPairs(keyPairs...),
		dbMap:  dbMap,
	}
	// clean db of expired sessions once a day
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour * 24):
				if err := s.destroyExpiredSessions(); err != nil {
					log.Warnf("destroyExpiredSessions: %v", err)
				}
			}
		}
	}()
	return s
}

// Get returns a cached session.
func (s *SQLStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New creates a new session for the given request r. If the request
// contains a valid session ID for an existing, non-expired session,
// then that session will be loaded from the database.
func (s *SQLStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	opts := *s.Options
	session.Options = &opts
	c, err := r.Cookie(name)
	if err != nil {
		if err == http.ErrNoCookie {
			return session, nil
		}
		return session, err
	}
	err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.codecs...)
	if err != nil {
		// these are not the sessions you are looking for
		log.Infof("sqlstore: New: unable to decode cookie: %v", err)
		return session, nil
	}
	err = s.load(session)
	if err != nil {
		return session, err
	}
	return session, nil
}

// Save stores the session in the database. If session.Options.MaxAge
// is < 0, the session is deleted from the database.
func (s *SQLStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge < 0 {
		return s.destroy(session)
	}
	if len(session.ID) == 0 {
		session.ID = base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32))
	}
	if err := s.save(session); err != nil {
		return err
	}
	// data is not stored in the cookie, only the session id
	encoded, err := securecookie.EncodeMulti(session.Name(), &session.ID, s.codecs...)
	if err != nil {
		return err
	}
	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

// load loads the session identified by its ID from the database if it
// exists. If the session has expired, it is destroyed.
func (s *SQLStore) load(session *sessions.Session) error {
	var dbSession models.Session
	if err := s.dbMap.SelectOne(&dbSession, "SELECT * FROM Session WHERE Token = ?", session.ID); err != nil {
		// if no rows are found nothing is done
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("Could not select session to destroy: %v", err)
	}
	if dbSession.Expires < time.Now().Unix() {
		return s.destroy(session)
	}
	// write db Data to session.Values
	return gob.NewDecoder(bytes.NewBuffer(dbSession.Data)).Decode(&session.Values)
}

// save checks whether the session is new and inserts if new. Updates if
// not.
func (s *SQLStore) save(session *sessions.Session) error {
	var dbSession models.Session
	var buf bytes.Buffer
	var isNew bool
	if err := s.dbMap.SelectOne(&dbSession, "SELECT * FROM Session WHERE Token = ?", session.ID); err != nil {
		if err != sql.ErrNoRows {
			return fmt.Errorf("Could not select session: %v", err)
		}
		// no rows found so new
		isNew = true
	}
	if userID, ok := session.Values["UserId"].(int64); ok {
		dbSession.UserId = userID
	} else {
		// all sessions with no user specified are UserId -1
		dbSession.UserId = -1
	}
	if err := gob.NewEncoder(&buf).Encode(session.Values); err != nil {
		return err
	}
	dbSession.Data = buf.Bytes()
	if isNew {
		now := time.Now().Unix()
		dbSession.Token = session.ID
		dbSession.Created = now
		dbSession.Expires = now + int64(session.Options.MaxAge)
		if err := s.dbMap.Insert(&dbSession); err != nil {
			return fmt.Errorf("Could not insert session: %v", err)
		}
	} else if _, err := s.dbMap.Update(&dbSession); err != nil {
		return fmt.Errorf("Could not update session: %v", err)
	}
	return nil
}

// delete one session from the db
func (s *SQLStore) destroy(session *sessions.Session) error {
	var dbSession models.Session
	if err := s.dbMap.SelectOne(&dbSession, "SELECT * FROM Session WHERE Token = ?", session.ID); err != nil {
		// if no rows are found nothing is done
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("Could not select session to destroy: %v", err)
	}
	if _, err := s.dbMap.Delete(&dbSession); err != nil {
		return fmt.Errorf("Could not destroy session: %v", err)
	}
	return nil
}

// delete expired sessions from the db
func (s *SQLStore) destroyExpiredSessions() error {
	var dbSession models.Session
	dbSessions, err := s.dbMap.Select(&dbSession, "SELECT * FROM Session WHERE Expires < ?", time.Now().Unix())
	if err != nil {
		return fmt.Errorf("Could not select expired sessions: %v", err)
	}
	_, err = s.dbMap.Delete(dbSessions...)
	if err != nil {
		return fmt.Errorf("Could not destroy expired sessions: %v", err)
	}
	return nil
}

// DestroySessionsForUserID deletes all sessions from the db for userId
//
// It should be noted that this does not prevent the user's current
// session from being saved again, which can be achieved by setting
// MaxAge to -1
func DestroySessionsForUserID(dbMap *gorp.DbMap, userID int64) error {
	var dbSession models.Session
	dbSessions, err := dbMap.Select(&dbSession, "SELECT * FROM Session WHERE UserId = ?", userID)
	if err != nil {
		return fmt.Errorf("Could not select user sessions to destroy: %v", err)
	}
	_, err = dbMap.Delete(dbSessions...)
	if err != nil {
		return fmt.Errorf("Could not destroy user sessions: %v", err)
	}
	return nil
}
