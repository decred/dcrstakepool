module github.com/decred/dcrstakepool

go 1.12

require (
	github.com/DATA-DOG/go-sqlmock v1.3.3
	github.com/apoydence/onpar v0.0.0-20190519213022-ee068f8ea4d1 // indirect
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
	github.com/decred/dcrwallet/rpc/jsonrpc/types v1.3.0
	github.com/decred/dcrwallet/wallet/v3 v3.0.0-20191015210243-b6a31b44514d
	github.com/decred/slog v1.0.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible
	github.com/go-gorp/gorp v2.0.1-0.20181104192722-f3677d4a0a88+incompatible
	github.com/go-sql-driver/mysql v1.4.1
	github.com/golang/protobuf v1.3.2
	github.com/gorilla/csrf v1.6.1
	github.com/gorilla/securecookie v1.1.1
	github.com/gorilla/sessions v1.2.0
	github.com/jessevdk/go-flags v1.4.0
	github.com/jrick/logrotate v1.0.0
	github.com/lib/pq v1.2.0 // indirect
	github.com/mattn/go-sqlite3 v1.11.0 // indirect
	github.com/poy/onpar v0.0.0-20190519213022-ee068f8ea4d1 // indirect
	github.com/stretchr/testify v1.4.0 // indirect
	github.com/zenazn/goji v0.9.0
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20191011191535-87dc89f01550
	golang.org/x/net v0.0.0-20191014212845-da9a3fd4c582
	google.golang.org/grpc v1.24.0
)

replace (
	github.com/decred/dcrwallet/deployments/v2 v2.0.0-00010101000000-000000000000 => github.com/decred/dcrwallet/deployments/v2 v2.0.0-20190927140119-e8a59aa721a4
	github.com/decred/dcrwallet/errors/v2 v2.0.0-00010101000000-000000000000 => github.com/decred/dcrwallet/errors/v2 v2.0.0-20190927140119-e8a59aa721a4
	github.com/decred/dcrwallet/rpc/client/dcrd v0.0.0-00010101000000-000000000000 => github.com/decred/dcrwallet/rpc/client/dcrd v0.0.0-20190927140119-e8a59aa721a4
)
