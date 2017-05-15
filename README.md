dcrstakepool
====

dcrstakepool is a web application which coordinates generating 1-of-2 multisig
addresses on a pool of [dcrwallet](https://github.com/decred/dcrwallet) servers
so users can purchase [proof-of-stake tickets](https://docs.decred.org/mining/proof-of-stake/)
on the [Decred](https://decred.org/) network and have the pool of wallet servers
vote on their behalf when the ticket is selected.

## v1.1.0 Architecture

![Stake Pool Architecture](https://i.imgur.com/2JDA9dl.png)

- It is highly recommended to use 3 dcrd+dcrwallet+stakepoold nodes for
  production use on mainnet.
- The architecture is subject to change in the future to lessen the dependence
  on dcrwallet and MySQL.

## 1.1.0 Release notes
  * Per-ticket votebits were removed in favor of per-user voting preferences.
    A voting page was added and the API upgraded to v2 to support getting and
    setting user voting preferences.
  * Addresses from the wallet servers which are needed for generating the 1-of-2
    multisig ticket address are now derived from the new votingwalletextpub
    config option. This removes the need to call getnewaddress on each wallet.
  * An experimental daemon (stakepoold) that votes according to user preference
    is available for testing on testnet. This daemon is not for use on mainnet
    at this time.

## 1.1.0 Mainnet Upgrade Guide

1) Upgrade dcrwallet and dcrstakepool
  * Upgrade to dcrwallet 1.0.0 or later. The deprecated enablestakemining option
  was removed.  Replace enablestakemining with enablevoting if it is still
  present in dcrwallet.conf.
  * Set votingwalletextpub in dcrstakepool.conf by getting the masterpubkey from
  the default account on a voting wallet per the instructions below.
  * [Update](#updating) dcrstakepool and its dependencies.
  * Start dcrstakepool. Verify that VoteBits and VoteBitsVersion
  columns were added to MySQL.  You should see the VoteBitsVersion being
  adjusted for all users to the stake version that dcrwallet is configured to
  use for voting (currently 4 on mainnet).  Also verify that the voting page
  shows the 2 mainnet agendas that are up for vote.
2) Enable stakepoold
  * Enable MySQL access from the stakepoold hosts.
  * Configure stakepoold by copying sample-stakepoold.conf to
  ~/.stakepoold/stakepoold.conf and editing it.  You should place stakepoold
  on the same server as dcrd/dcrwallet so stakepoold can talk to them via
  loopback to lower latency as much as possible.
  * Copy stakepoold certs to the dcrstakepool server and set
  enablestakepoold=true, stakepooldhosts, stakepooldcerts.
  * Once stakepoold is setup and verified to be voting, disable enablevoting=1 in
  dcrwallet.conf and restart dcrwallet.

## Requirements

- [Go](http://golang.org) 1.7.0 or newer.
- MySQL
- Nginx or other web server to proxy to dcrstakepool

## Installation

#### Linux/BSD/MacOSX/POSIX - Build from Source

Building or updating from source requires the following build dependencies:

- **Go 1.7.0 or newer**

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
$ git clone https://github.com/decred/dcrstakepool $GOPATH/src/github.com/decred/dcrstakepool
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ glide install
```

- Assuming you have done the below configuration, build and run dcrstakepool:

```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ go build
$ ./dcrstakepool
```

- Build stakepoold and copy it to your voting nodes:

```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool/backend/stakepoold
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
$ cd $GOPATH/src/github.com/decred/dcrstakepool/backend/stakepoold
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

- Get the master pubkey from the default account.  This will be used for votingwalletextpub in dcrstakepool.conf.

```bash
$ dcrctl --wallet getmasterpubkey default
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
```

- Copy sample config and edit appropriately
```bash
$ cp -p sample-dcrstakepool.conf dcrstakepool.conf
```

## Running

The easiest way to run the stakepool code is to run it directly from the root of
the source tree:

```bash
$ cd $GOPATH/src/github.com/decred/dcrstakepool
$ go build
$ ./dcrstakepool
```

If you wish to run dcrstakepool from a different directory you will need to change **publicpath** and **templatepath**
from their relative paths to an absolute path.

## Development

If you are modifying templates, sending the USR1 signal to the dcrstakepool process will trigger a template reload.

## Operations

- dcrstakepool will connect to the database or error out if it cannot do so

- dcrstakepool will create the stakepool.Users table automatically if it doesn't exist

- dcrstakepool attempts to connect to all of the wallet servers on startup or error out if it cannot do so

- dcrstakepool takes a user's pubkey, validates it, calls getnewaddress on all the wallet servers, then createmultisig, and finally importscript.  If any of these RPCs fail or returns inconsistent results, the RPC client built-in to dcrstakepool will shut down and will not operate until it has been restarted.  Wallets should be verified to be in sync before restarting.

- User API Tokens have an issuer field set to baseURL from the configuration file.
  Changing the baseURL requires all API Tokens to be re-generated.

## Adding Invalid Tickets

If a user pays an incorrect fee you may add their tickets like so (requires dcrd running with txindex=1):

```bash
dcrctl --wallet stakepooluserinfo "MultiSigAddress" | grep -Pzo '(?<="invalid": \[)[^\]]*' | tr -d , | xargs -Itickethash dcrctl --wallet getrawtransaction tickethash | xargs -Itickethex dcrctl --wallet addticket "tickethex"
```

## Backups, monitoring, security considerations

- MySQL should be backed up often and regularly (probably at least hourly). Backups should be transferred off-site.  If using binary backups, do a test restore. For .sql files, verify visually.

- A monitoring system with alerting should be pointed at dcrstakepool and tested/verified to be operating properly.  There is a hidden /status page which throws 500 if the RPC client is shutdown.  If your monitoring system supports it, add additional points of verification such as: checking that the /stats page loads and has expected information in it, create a test account and setup automated login testing, etc.

- Wallets should never be used for anything else (they should always have a balance of 0)

## Disaster Recovery

**Always keep at least one wallet voting while performing maintenance / restoration!**

- In the case of a total failure of a wallet server:
  * Restore the failed wallet(s) from seed
  * Restart the dcrstakepool process to allow automatic syncing to occur.

## IRC

- irc.freenode.net
- channel #decred

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrstakepool/issues)
is used for this project.

## License

dcrstakepool is licensed under the [copyfree](http://copyfree.org) ISC License.

## Version History
- 1.0.0 - Major changes/improvements.
  * API is now at v1 status.  API Tokens are generated for all users with a
    verified email address when upgrading.  Tokens are generated for new
    users on demand when visiting the Settings page which displays their token.
    Authenticated users may use the API to submit a public key address and to
    retrieve ticket purchasing information.  The stake pool's stats are also
    available through the API without authentication.
- 0.0.4 - Major changes/improvements.
  * config.toml is no longer required as the options in that file have been
    migrated to dcrstakepool.conf.
  * Automatic syncing of scripts, tickets, and vote bits is now performed at
    startup.  Syncing of vote bits is a long process and can be skipped with the
    SkipVoteBitsSync flag/configuration value.
  * Temporary wallet connectivity errors are now handled much more gracefully.
  * A preliminary v0.1 API was added.
- 0.0.3 - More expected/basic web application functionality added.
  * SMTPHost now defaults to an empty string so a stake pool can be used for
    development or testing purposes without a configured mail server.  The
    contents of the emails are sent through the logger so links can still be
    followed.
  * Upon sign up, users now have an email sent with a validation link.
    They will not be able to sign in until they verify.
  * New settings page that allows users to change their email address/password.
  * Bug fix to HeightRegistered migration for users who signed up but never
    submitted an address would not be able to login.
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
