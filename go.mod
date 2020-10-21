module github.com/decred/dcrstakepool

go 1.13

require (
	decred.org/dcrwallet v1.6.0-rc1
	github.com/DATA-DOG/go-sqlmock v1.4.1
	github.com/dajohi/goemail v1.0.1
	github.com/dchest/captcha v0.0.0-20170622155422-6a29415a8364
	github.com/decred/dcrd/blockchain/stake/v3 v3.0.0
	github.com/decred/dcrd/certgen v1.1.1
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v3 v3.0.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrutil/v3 v3.0.0
	github.com/decred/dcrd/hdkeychain/v3 v3.0.0
	github.com/decred/dcrd/rpc/jsonrpc/types/v2 v2.1.0
	github.com/decred/dcrd/rpcclient/v6 v6.0.0
	github.com/decred/dcrd/wire v1.4.0
	github.com/decred/dcrdata/api/types/v5 v5.0.1
	github.com/decred/dcrdata/db/dbtypes/v2 v2.2.1
	github.com/decred/slog v1.1.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/golang/protobuf v1.4.3
	github.com/gorilla/csrf v1.6.2
	github.com/gorilla/securecookie v1.1.1
	github.com/gorilla/sessions v1.2.0
	github.com/jessevdk/go-flags v1.4.1-0.20200711081900-c17162fe8fd7
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.3.0 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/poy/onpar v0.0.0-20190519213022-ee068f8ea4d1 // indirect
	github.com/stretchr/testify v1.5.1 // indirect
	github.com/zenazn/goji v0.9.0
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a
	google.golang.org/grpc v1.32.0
)
