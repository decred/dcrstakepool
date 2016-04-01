dcrstakepool
====

dcrstakepool is a minimalist web application which provides a method for allowing users to purchase tickets and have a pool of wallet servers redeem and vote on the user's behalf.

## Requirements

- [Go](http://golang.org) 1.5.3 or newer.
- MySQL
- Nginx or other web server to proxy to dcrstakepool

## Installation

#### Linux/BSD/MacOSX/POSIX - Build from Source

- Install Go according to the installation instructions here:
  http://golang.org/doc/install

- Ensure Go was installed properly and is a supported version:

```bash
$ go version
$ go env GOROOT GOPATH
```

NOTE: The `GOROOT` and `GOPATH` above must not be the same path.  It is
recommended that `GOPATH` is set to a directory in your home directory such as
`~/goprojects` to avoid write permission issues.

- Run the following command to obtain dcrstakepool, all dependencies, and install it:

```bash
$ cd $GOPATH/src/github.com/decred
$ git clone git@github.com:decred/dcrstakepool-private.git dcrstakepool
$ cd dcrstakepool
$ go get -u ./...
```

- dcrstakepool (and utilities) will now be installed in either ```$GOROOT/bin``` or
  ```$GOPATH/bin``` depending on your configuration.  If you did not already
  add the bin directory to your system path during Go installation, we
  recommend you do so now.

## Setup

#### Pre-requisites

These instructions assume you are familiar with dcrd/dcrwallet.

- Create basic dcrd/dcrwallet/dcrctl config files with usernames, passwords, rpclisten, and network set appropriately within them or run example commands with additional flags as necessary

- Build/install dcrd and dcrwallet from latest master

- Run dcrd instances and let them fully sync

#### dcrwallet

- Create the wallets.  All wallets should have the same seed.  **Backup the seed for disaster recovery!**

```bash
$ dcrwallet --create
```

- Start dcrwallet with debug on (this prints the position of the address index which is useful if getnewaddress fails and the wallets get de-synced)

```bash
$ dcrwallet -d debug
```

- Unlock the wallet

```bash
$ dcrctl --wallet walletpassphrase pass 0
```

#### MySQL

- Install, configure, and start MySQL

- Add stakepool user and create the stakepool database

```bash
$ mysql -uroot -ppassword

MySQL> CREATE USER 'stakepool'@'localhost' IDENTIFIED BY 'password';
MySQL> GRANT ALL PRIVILEGES ON *.* TO '%' WITH GRANT OPTION;
MySQL> FLUSH PRIVILEGES;
MySQL> CREATE DATABASE stakepool;
```

#### Nginx/web server

- Adapt sample-nginx.conf or setup a different web server in a proxy configuration

#### dcrstakepool

- Register a recaptcha key for your domain at https://www.google.com/recaptcha/admin

- Generate a secret for cookie authentication

```bash
$ openssl rand -hex 32
```

- add wallet server information to controllers/config.go

- Create the .dcrstakepool directory and copy dcrwallet certs to it
```bash
$ mkdir ~/.dcrstakepool
$ cd ~/.dcrstakepool
$ scp walletserver1:~/.dcrwallet/rpc.cert wallet1.cert
$ scp walletserver2:~/.dcrwallet/rpc.cert wallet2.cert
$ scp walletserver3:~/.dcrwallet/rpc.cert wallet3.cert
```

- add recaptcha secret to controllers/main.go and recaptcha sitekey to views/auth/signup.html or disable captcha if you don't want it

- copy sample config and edit appropriately
```bash
$ cp -p config.toml.sample config.toml
```

- Run dcrstakepool
```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ ./dcrstakepool
```

## Operations

- dcrstakepool will connect to the database or error out if it cannot do so

- dcrstakepool will create the stakepool.Users table automatically if it doesn't exist

- dcrstakepool attempts to connect to all of the wallet servers on startup and or error out if it cannot do so

- dcrstakepool takes a user's pubkey, validates it, calls getnewaddress on all the wallet servers, then createmultisig, and finally importscript.  If any of these RPCs fail or returns inconsistent results, the RPC client built-in to dcrstakepool will shut down and will not operate until it has been restarted.  Wallets should be verified to be in sync before restarting.

## Backups, monitoring, security considerations

- MySQL should be backed up often and regularly (probably at least hourly). Backups should be transferred off-site.  If using binary backups, do a test restore. For .sql files, verify visually.

- A monitoring system with alerting should be pointed at dcrstakepool and tested/verified to be operating properly.  There is a hidden /status page which throws 500 if the RPC client is shutdown.  If your monitoring system supports it, add additional points of verification such as: checking that the /stats page loads and has expected information in it, create a test account and setup automated login testing, etc.

- Wallets should never be used for anything else (they should always have a balance of 0)

## Disaster Recovery

**Always keep at least one wallet voting while performing maintenance / restoration!**

- In the case of a total failure of a wallet server:
  * Restore the failed wallet(s) from seed
  * getnewaddress until it matches the index of the other wallets
  * Import scripts for all users
```bash
for s in `mysql -ustakepool -ppassword -Dstakepool -se 'SELECT Multisigscript FROM Users WHERE LENGTH(Multisigscript) != 0'`;
do dcrctl --wallet importscript "$s";
done
```

- If servers get desynced but are otherwise running okay, call getnewaddress/listscripts/importscript until they're re-synced.  See doc.go for some example shell commands.

## TODO

- vendor goji
- fix flags conflict between goji/dcrstakepool
- finish unified config file

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrstakepool-private/issues)
is used for this project.

## License

dcrstakepool is licensed under the [copyfree](http://copyfree.org) ISC License.
