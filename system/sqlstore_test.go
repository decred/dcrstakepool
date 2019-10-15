package system

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/base32"
	"encoding/gob"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/decred/dcrstakepool/models"
	"github.com/go-gorp/gorp"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

// tokens used for sessions
func newTokens(n int) []string {
	toks := make([]string, n)
	for i := range toks {
		toks[i] = base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32))
	}
	return toks
}

// Get Data representation of session.Values. Currently only testing for
// user id
func gobFromValues(i int64) []byte {
	m := map[interface{}]interface{}{"UserId": i}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(m)
	return buf.Bytes()
}

// dbSession.Data with no... data
func nilGob() []byte {
	m := map[interface{}]interface{}{}
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(m)
	return buf.Bytes()
}

// helper for sqlmock select
func expectSelect(mock sqlmock.Sqlmock, args []driver.Value, rows *sqlmock.Rows, err error) {
	mock.ExpectQuery(`^SELECT (.*) FROM Session WHERE Token = (.+)$`).
		WithArgs(args...).
		WillReturnRows(rows).
		WillReturnError(err)
}

// helper for sqlmock delete
func expectDelete(mock sqlmock.Sqlmock, args []driver.Value) {
	mock.ExpectExec("^delete from `Session` where `SessionID`=(.+)$").
		WithArgs(args...).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// helper for sqlmock update
func expectUpdate(mock sqlmock.Sqlmock, args []driver.Value) {
	mock.ExpectExec("^update `Session` set `Token`=(.+), `Data`=(.+), `UserId`=(.+), `Created`=(.+), `Expires`=(.+) where `SessionID`=(.+);$").
		WithArgs(args...).
		WillReturnResult(sqlmock.NewResult(0, 0))
}

// setup db, mock db, and sqlstore
func makeDbAndStore() (sqlmock.Sqlmock, *sql.DB, *SQLStore) {
	var wg sync.WaitGroup
	ctx := context.Background()
	// Open new mock database
	db, mock, err := sqlmock.New()
	if err != nil {
		panic(err)
	}
	dbMap := &gorp.DbMap{
		Db:              db,
		Dialect:         gorp.MySQLDialect{Engine: "InnoDB", Encoding: "UTF8MB4"},
		ExpandSliceArgs: true,
	}
	dbMap.AddTableWithName(models.Session{}, "Session").SetKeys(true, "Id")
	hash := sha256.New()
	io.WriteString(hash, "abrakadabra")
	s := NewSQLStore(ctx, &wg, dbMap, hash.Sum(nil))
	s.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		//six hours
		MaxAge: 60 * 60 * 6,
	}
	return mock, db, s
}

// add session token to request cookies
func setSessionForUserID(r *http.Request, store *SQLStore, maxAge int, userID int64) *sessions.Session {
	session := sessions.NewSession(store, "session")
	session.Values["UserId"] = userID
	session.ID = tokens[userID]
	opts := *store.Options
	opts.MaxAge = maxAge
	session.Options = &opts
	encoded, err := securecookie.EncodeMulti(session.Name(), &session.ID, store.codecs...)
	if err != nil {
		panic(err)
	}
	cookie := sessions.NewCookie(session.Name(), encoded, session.Options)
	if r != nil {
		r.AddCookie(cookie)
	}
	return session
}

var (
	tokens          = newTokens(4)
	now             = time.Now().Unix()
	oneDay    int64 = 60 * 60 * 24
	yesterday       = now - oneDay
	tomorrow        = now + oneDay
	col             = []string{"Token", "Data", "UserId", "Created", "Expires", "SessionID"}
)

type testNew struct {
	userID     int64
	hasCookie  bool
	isExpired  bool
	args       []driver.Value
	row        []driver.Value
	err        error
	sessValues map[interface{}]interface{}
}

var testsNew = []testNew{
	{0, true, false, []driver.Value{tokens[0]}, []driver.Value{tokens[0], gobFromValues(0), 0, now, tomorrow, 0}, nil, map[interface{}]interface{}{"UserId": int64(0)}},
	//expired
	{1, true, true, []driver.Value{tokens[1]}, []driver.Value{tokens[1], gobFromValues(1), 1, now, yesterday, 0}, nil, map[interface{}]interface{}{}},
	//no cookie in request
	{2, false, false, []driver.Value{tokens[2]}, []driver.Value{tokens[2], gobFromValues(2), 2, now, tomorrow, 0}, nil, map[interface{}]interface{}{}},
	//no rows
	{3, true, false, []driver.Value{tokens[3]}, []driver.Value{tokens[3], gobFromValues(3), 3, now, tomorrow, 0}, sql.ErrNoRows, map[interface{}]interface{}{}},
}

func TestNew(t *testing.T) {
	mock, db, store := makeDbAndStore()
	defer db.Close()
	for _, test := range testsNew {
		r, err := http.NewRequest("GET", "http://localhost/blah", nil)
		if err != nil {
			t.Error(err)
		}
		// cookie was previously saved
		if test.hasCookie {
			setSessionForUserID(r, store, 60, test.userID)
			expectSelect(mock, test.args, sqlmock.NewRows(col).AddRow(test.row...), test.err)
			// session is expired in db
			if test.isExpired {
				expectSelect(mock, test.args, sqlmock.NewRows(col).AddRow(test.row...), test.err)
				expectDelete(mock, []driver.Value{0})
			}
		}
		// testing
		s, err := store.New(r, "session")
		if err != nil {
			t.Errorf("session load err: %v ", err)
		}
		// expected session.Values must equal actual
		eq := reflect.DeepEqual(s.Values, test.sessValues)
		if !eq {
			t.Errorf("expected session values %v but got %v", test.sessValues, s.Values)
		}
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectation error: %s", err)
		}
	}
}

type testSave struct {
	userID      int64
	hasUserID   bool
	isNew       bool
	isDestroyed bool
	maxAge      int
	args        []driver.Value
	row         []driver.Value
	err         error
}

var testsSave = []testSave{
	{0, true, false, false, 60, []driver.Value{tokens[0]}, []driver.Value{tokens[0], gobFromValues(0), 0, now, tomorrow, 0}, nil},
	// maxAge is -1
	{1, true, false, true, -1, []driver.Value{tokens[1]}, []driver.Value{tokens[1], gobFromValues(1), 1, now, tomorrow, 0}, nil},
	// is new with no user id
	{0, false, true, false, 60, []driver.Value{sqlmock.AnyArg(), nilGob(), -1, sqlmock.AnyArg(), sqlmock.AnyArg()}, []driver.Value{}, sql.ErrNoRows},
	// is new with user id
	{2, true, true, false, 60, []driver.Value{sqlmock.AnyArg(), gobFromValues(2), 2, sqlmock.AnyArg(), sqlmock.AnyArg()}, []driver.Value{}, sql.ErrNoRows},
}

func TestSave(t *testing.T) {
	mock, db, store := makeDbAndStore()
	defer db.Close()
	for _, test := range testsSave {
		r, err := http.NewRequest("GET", "http://localhost/blah", nil)
		w := httptest.NewRecorder()
		s := sessions.NewSession(store, "session")
		if err != nil {
			t.Error(err)
		}
		// cookie was previously saved
		if test.hasUserID {
			// save doesn't matter what's in the request
			s = setSessionForUserID(nil, store, test.maxAge, test.userID)
		}
		// maxAge of -1 is destroyed
		if test.isDestroyed {
			expectSelect(mock, test.args, sqlmock.NewRows(col).AddRow(test.row...), test.err)
			expectDelete(mock, []driver.Value{0})
		} else {
			if test.isNew {
				// a new session is inserted, we cant be sure of the exact ID and time
				mock.ExpectQuery(`^SELECT (.*) FROM Session WHERE Token = (.+)$`).
					WillReturnError(test.err)
				mock.ExpectExec("^insert into `Session` \\(`SessionID`,`Token`,`Data`,`UserId`,`Created`,`Expires`\\) values \\(null,(.+),(.+),(.+),(.+),(.+)\\);$").
					WithArgs(test.args...).
					WillReturnResult(sqlmock.NewResult(0, 0))
			} else {
				// a found session is updated
				expectSelect(mock, test.args, sqlmock.NewRows(col).AddRow(test.row...), test.err)
				expectUpdate(mock, test.row)
			}
		}
		// testing
		err = store.Save(r, w, s)
		if err != nil {
			t.Errorf("session save err: %v ", err)
		}
		// if the database transactions went as expected pass
		if err = mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet expectation error: %s", err)
		}
	}
}
