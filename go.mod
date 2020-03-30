module github.com/decred/dcrstakepool

go 1.13

require (
	github.com/DATA-DOG/go-sqlmock v1.4.1
	github.com/dajohi/goemail v1.0.1
	github.com/dchest/captcha v0.0.0-20170622155422-6a29415a8364
	github.com/decred/dcrd/blockchain/stake/v2 v2.0.2
	github.com/decred/dcrd/certgen v1.1.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.2
	github.com/decred/dcrd/chaincfg/v2 v2.3.0
	github.com/decred/dcrd/dcrec v1.0.0
	github.com/decred/dcrd/dcrutil/v2 v2.0.1
	github.com/decred/dcrd/hdkeychain/v2 v2.1.0
	github.com/decred/dcrd/rpcclient/v4 v4.0.0
	github.com/decred/dcrd/wire v1.3.0
	github.com/decred/dcrdata/api/types/v4 v4.0.4
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.4.0
	github.com/decred/dcrwallet/wallet/v3 v3.2.1
	github.com/decred/slog v1.0.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/golang/protobuf v1.3.4
	github.com/gorilla/csrf v1.6.2
	github.com/gorilla/securecookie v1.1.1
	github.com/gorilla/sessions v1.2.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.3.0 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/poy/onpar v0.0.0-20190519213022-ee068f8ea4d1 // indirect
	github.com/stretchr/testify v1.5.1 // indirect
	github.com/zenazn/goji v0.9.0
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20200302210943-78000ba7a073
	google.golang.org/grpc v1.27.1
)
