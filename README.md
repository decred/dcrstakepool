dcrstakepool
====

dcrstakepool is a minimalist web application which provides a method for allowing users to purchase tickets and have a pool of wallet servers redeem and vote on the user's behalf.

## Version History

- 0.0.2 - Minor improvements/feature addition
  * The importscript RPC is now called with the current block height at the
    time of user registration. Previously, importscript triggered a rescan
    for transactions from the genesis block.  Since the user just registered,
    there won't be any transactions present.  A new HeightRegistered column
    is automatically added to the Users table.  A default value of 15346 is
    used for existing users who already had a multisigscript generated.
    This can be adjusted to a more reasonable value for you pool by running
    the following MySQL query:
    ```UPDATE Users SET HeightRegistered = NEWVALUE WHERE HeightRegistered = 15346;```
  * Users may now reset their password by specifying an email address and
    clicking a link that they will receive via email.  You will need to
    add a proper configuration for your mail server for it to work properly.
    The various SMTP options can be seen in **sample-dcrstakepool.conf**.
  * User instructions on the address and ticket pages were updated.
  * SpentBy link added to the voted tickets display.
- 0.0.1 - Initial release for mainnet operations

## Requirements

- [Go](http://golang.org) 1.6.3 or newer.
- MySQL
- Nginx or other web server to proxy to dcrstakepool

## Installation

#### Linux/BSD/MacOSX/POSIX - Build from Source

Building or updating from source requires the following build dependencies:

- **Go 1.6 or 1.7**

  Installation instructions can be found here: http://golang.org/doc/install.
  It is recommended to add `$GOPATH/bin` to your `PATH` at this point.

- **Glide**

  Glide is used to manage project dependencies and provide reproducible builds.
  To install:

  `go get -u github.com/Masterminds/glide`

Unfortunately, the use of `glide` prevents a handy tool such as `go get` from
automatically downloading, building, and installing the source in a single
command.  Instead, the latest project and dependency sources must be first
obtained manually with `git` and `glide`, and then `go` is used to build and
install the project.

- Run the following command to obtain the dcrstakepool code and all dependencies:

```bash
$ git clone https://github.com/decred/dcrstakepool-private $GOPATH/src/github.com/decred/dcrstakepool
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ glide install
```

- Assuming you have done the below configuration, build and run dcrstakepool:

```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ go build
```

## Updating

To update an existing source tree, pull the latest changes and install the
matching dependencies:

```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ git pull
$ glide install
$ go build
```

## Setup

#### Pre-requisites

These instructions assume you are familiar with dcrd/dcrwallet.

- Create basic dcrd/dcrwallet/dcrctl config files with usernames, passwords, rpclisten, and network set appropriately within them or run example commands with additional flags as necessary

- Build/install dcrd and dcrwallet from latest master

- Run dcrd instances and let them fully sync

#### Stake pool fees/cold wallet

- Setup a new wallet for receiving payment for stake pool fees.  **This should be completely separate from the stake pool infrastructure.**

```bash
$ dcrwallet --create
$ dcrwallet
```

- Get the master pubkey for the account you wish to use. This will be needed to configure dcrwallet and dcrstakepool.

```bash
$ dcrctl --wallet createnewaccount teststakepoolfees
$ dcrctl --wallet getmasterpubkey teststakepoolfees
```

- Mark 10000 addresses in use for the account so the wallet will recognize transactions to those addresses. Fees from UserId 1 will go to address 1, UserId 2 to address 2, and so on.

```bash
$ dcrctl --wallet accountsyncaddressindex teststakepoolfees 0 10000
```

#### Stake pool voting wallets

- Create the wallets.  All wallets should have the same seed.  **Backup the seed for disaster recovery!**

```bash
$ dcrwallet --create
```

- Start a properly configured dcrwallet and unlock it. See sample-dcrwallet.conf.

```bash
$ dcrwallet
```

#### MySQL

- Install, configure, and start MySQL

- Add stakepool user and create the stakepool database

```bash
$ mysql -uroot -ppassword

MySQL> CREATE USER 'stakepool'@'localhost' IDENTIFIED BY 'password';
MySQL> GRANT ALL PRIVILEGES ON *.* TO 'stakepool'@'localhost' WITH GRANT OPTION;
MySQL> FLUSH PRIVILEGES;
MySQL> CREATE DATABASE stakepool;
```

#### Nginx/web server

- Adapt sample-nginx.conf or setup a different web server in a proxy configuration

#### dcrstakepool

- Create the .dcrstakepool directory and copy dcrwallet certs to it
```bash
$ mkdir ~/.dcrstakepool
$ cd ~/.dcrstakepool
$ scp walletserver1:~/.dcrwallet/rpc.cert wallet1.cert
$ scp walletserver2:~/.dcrwallet/rpc.cert wallet2.cert
$ scp walletserver3:~/.dcrwallet/rpc.cert wallet3.cert
```

- Copy old-style sample config and edit appropriately
```bash
$ cp -p config.toml.sample config.toml
```

- Copy new-style sample config and edit appropriately
```bash
$ cp -p sample-dcrstakepool.conf dcrstakepool.conf
```

## Running

The easiest way to run the stakepool code is to run it directly from the root of
the source tree:

```
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ go build
$ ./dcrstakepool
```

If you wish to run dcrstakepool from a different directory you will need to:

1) Copy **config.toml** to the same directory you will be running **dcrstakepool**
   from
2) Either copy **public** and **views** to the same directory or specify
   absolute paths in **config.toml**

## Operations

- dcrstakepool will connect to the database or error out if it cannot do so

- dcrstakepool will create the stakepool.Users table automatically if it doesn't exist

- dcrstakepool attempts to connect to all of the wallet servers on startup or error out if it cannot do so

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

## IRC

- irc.freenode.net
- channel #decred-stakepool (requires registered nickname and an invite -- to get an invite PM jolan on forum/freenode after registering with NickServ)

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrstakepool-private/issues)
is used for this project.

## License

dcrstakepool is licensed under the [copyfree](http://copyfree.org) ISC License.
