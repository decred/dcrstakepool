# dcrstakepool

[![GoDoc](https://godoc.org/github.com/decred/dcrstakepool?status.svg)](https://godoc.org/github.com/decred/dcrstakepool)
[![Build Status](https://travis-ci.org/decred/dcrstakepool.svg?branch=master)](https://travis-ci.org/decred/dcrstakepool)
[![Go Report Card](https://goreportcard.com/badge/github.com/decred/dcrstakepool)](https://goreportcard.com/report/github.com/decred/dcrstakepool)

dcrstakepool is a web application which coordinates generating 1-of-2 multisig
addresses on a pool of [dcrwallet](https://github.com/decred/dcrwallet) servers
so users can purchase [proof-of-stake tickets](https://docs.decred.org/mining/proof-of-stake/)
on the [Decred](https://decred.org/) network and have the pool of wallet servers
vote on their behalf when the ticket is selected.

## Architecture

![Voting Service Architecture](https://i.imgur.com/2JDA9dl.png)

- It is highly recommended to use 3 dcrd+dcrwallet+stakepoold nodes for
  production use on mainnet.
- The architecture is subject to change in the future to lessen the dependence
  on dcrwallet and MySQL.

## Getting ready to run dcrstakepool

### Requirements
Before running dcrstakepool, have the following ready:
- Frontend server - for hosting and serving the http frontend and Rest API.
- Backend server(s) - for hosting the voting wallet(s).
- Decred binaries (dcrd, dcrwallet and dcrctl).
[Build each binary from its source code](#Building binaries from source) or
[download the latest release from here](https://github.com/decred/decred-binaries/releases), look under the **Assets** section of the latest release for download links.
  - The latest release binaries of dcrd, dcrwallet and dcrctl may not work with the current `dcrstakepool` source code.
If you encounter rpc version mismatch errors using the latest release decred binaries,
follow [these steps](#Building binaries from source) to build each binary from source instead of using the release binaries.
- VSP binaries (dcrstakepool and stakepoold).
No release versions available yet, [build both binaries from source code](#Building binaries from source).

_**Important notes:**_
- For testing purposes, one server may be used for both frontend and backend.
It is however recommended to use different physical servers for production.
- The required decred and VSP binaries DO NOT need to be built on the server(s).
They can be built from a development machine and copied to each server.

## Running dcrstakepool

### Set up a cold wallet for VSP fee collection
- - Configure `dcrd`, `dcrwallet` and `dcrctl`.
  The minimum setup required is described [here](https://docs.decred.org/advanced/manual-cli-install/#minimum-configuration).
- Run `dcrd` and let it sync fully.
- Run `dcrwallet --create` to create the fee collection wallet.
- (Optional) Create a new account in the wallet for use in generating fee addresses:
```bash
dcrctl --wallet createnewaccount vspfees
```
- Mark 10000 addresses in use for the account so the wallet will recognize transactions to those addresses.
Fees from UserId 1 will go to address 1, UserId 2 to address 2, and so on.
```bash
dcrctl --wallet accountsyncaddressindex vspfees 0 10000
```
- Get the master pubkey for the account you wish to use.
This will be needed to configure dcrwallet and dcrstakepool (`coldwalletextpub`).
```bash
dcrctl --wallet getmasterpubkey vspfees
```

### Set up the backend server(s)
Perform the following steps on each backend server, where voting will occur:

#### Set up the voting wallet
- Copy `dcrd`, `dcrwallet` and `dcrctl` to the backend server and configure them.
The minimum configuration required is described [here](https://docs.decred.org/advanced/manual-cli-install/#minimum-configuration).
- See [sample-dcrwallet.conf](sample-dcrwallet.conf) for other required `dcrwallet` configuration options.
- Run `dcrd` and let it sync fully.
- Run `dcrwallet --create` to create the voting wallet.
**IMPORTANT: Use the same seed for all voting wallets. Backup the seed for disaster recovery!** 
- Start the `dcrwallet` daemon and unlock the wallet by providing your wallet's private passphrase: `dcrwallet --promptpass`
- Get the master pubkey from the default account. This will be used for votingwalletextpub in dcrstakepool.conf:
```bash
dcrctl --wallet getmasterpubkey default
```
- Make a copy of your dcrwallet rpc username, password and rpc.cert file.
These will be required when [setting up the frontend server](#Set up the frontend server).

#### Set up MySQL
- Install MySQL.
- Setup MySQL user credentials and create stakepool database:
```bash
$ mysql -u root -p password
MySQL> CREATE USER 'stakepool'@'localhost' IDENTIFIED BY 'password';
MySQL> GRANT ALL PRIVILEGES ON *.* TO 'stakepool'@'localhost' WITH GRANT OPTION;
MySQL> FLUSH PRIVILEGES;
MySQL> CREATE DATABASE stakepool;
```

#### Set up and run stakepoold
- Copy the `stakepoold` binary to the backend server.
- Run `stakepoold -h` to see the stakepoold config file location on your server.
- Copy [sample-stakepoold.conf](sample-stakepoold.conf) to the location above.
- Or create the config file at the config location gotten above
and copy the contents of [sample-stakepoold.conf](sample-stakepoold.conf) into the new file.
- Edit the config file as appropriate.
Following config values must be set:
  - `coldwalletextpub` - gotten from [this setup step](#Set up a cold wallet for VSP fee collection)
  - `dbpassword` - password set [during MySQL setup](#Set up MySQL)
  - `dcrduser`, `dcrdpass`, `walletuser`, `walletpassword` - dcrd/dcrwallet rpc auth values
  configured while [setting up the voting wallet](#Set up the voting wallet)
- Run `stakepoold`.
- Make a copy of your stakepoold rpc.cert file.
It will be required when [setting up the frontend server](#Set up the frontend server).

### Set up the frontend server

#### Set up MySQL
- Install MySQL.
- Setup MySQL user credentials and create stakepool database:
```bash
$ mysql -u root -p password
MySQL> CREATE USER 'stakepool'@'localhost' IDENTIFIED BY 'password';
MySQL> GRANT ALL PRIVILEGES ON *.* TO 'stakepool'@'localhost' WITH GRANT OPTION;
MySQL> FLUSH PRIVILEGES;
MySQL> CREATE DATABASE stakepool;
```

#### Set up and run dcrstakepool
- Copy the `dcrstakepool` binary to the backend server.
- Also copy the [public](public) and [views](views) folders to the server,
preferably the same location as the `dcrstakepool` binary.
This step is unnecessary if dcrstakepool source code exists on the server,
even if the binary is in a different location from the source code.
- Run `dcrstakepool -h` to see the dcrstakepool config file location on your server.
- Copy [sample-dcrstakepool.conf](sample-dcrstakepool.conf) to the location above.
- Or create the config file at the config location gotten above
and copy the contents of [sample-dcrstakepool.conf](sample-dcrstakepool.conf) into the new file.
- Edit the config file as appropriate.
Following config values must be set:
  - `apisecret` - Secret string used to encrypt API and to generate CSRF tokens.
  Can use `openssl rand -hex 32` to generate one.
  - `cookiesecret` - Secret string used to encrypt session data.
  Can use `openssl rand -hex 32` to generate one.
  - `dbpassword` - password set [during MySQL setup](#Set up MySQL)
  - `votingwalletextpub` - Extended public key used to generate ticketed addresses which are
  combined with a user address for 1-of-2 multisig.
  Should have been copied [while setting up the voting wallet on the server](#Set up the voting wallet).
  - `coldwalletextpub` - Extended public key used to generate fee payment addresses,
  gotten from [this setup step](#Set up a cold wallet for VSP fee collection).
  - `poolfees` - Fees as a percentage. 7.5 = 7.5%.  Precision of 2, 7.99 = 7.99%.
  Should match dcrwallet's configuration (refer to [the second bullet point here](#Set up the voting wallet)).
  - `stakepooldhosts` - IP address for all backend servers, separated by comma.
  Important to enable access to the stakepoold port on each backend server.
  - `stakepooldcerts` - relative or absolute path to rpc cert files for all backend servers, separated by comma.
  Each backend rpc cert file should have been copied [while setting up stakepoold on the server](#Set up and run stakepoold).
  - `wallethosts` - IP address for the dcrwallet daemons on all backend servers, separated by comma.
  Important to enable access to the dcrwallet port on each backend server.
  - `walletcerts` - relative or absolute path to rpc cert files for the dcrwallet daemons on all backend servers, separated by comma.
  Each rpc cert file should have been copied [while setting up the voting wallet on the server](#Set up the voting wallet).
  - `walletusers`, `walletpasswords` - comma separated list of rpc username and password for the dcrwallet daemons on all backend servers.
  These info should have been copied/noted down [while setting up the voting wallet on the server](#Set up the voting wallet).
- If the `dcrstakepool` binary is not in the same directory as the [public](public) and [views](views) folders,
you will need to change `publicpath` and `templatepath` from their relative paths to an absolute path in `dcrstakepool.conf`.
- Run `dcrstakepool`.
- Rest API and VSP frontend should be ready for use.

## Supplementary info

### Building binaries from source
_PS: This assumes that you have installed Go 1.11 or later and you have added `$GOPATH/bin` to your `PATH` environment variable.
Please do so before proceeding if you haven't already.
You can access the go installation guide [here](http://golang.org/doc/install)._

#### Decred binaries from source
The following bash code builds the dcrd and dcrctl binaries and places them in `$GOPATH/src/github.com/decred/dcrd`.
Replace `go build` with `go install` to place the binaries in `$GOPATH/bin`.
```bash
git clone https://github.com/decred/dcrd $GOPATH/src/github.com/decred/dcrd
cd $GOPATH/src/github.com/decred/dcrd
GO111MODULE=on go build && go build ./cmd/dcrctl
```

The following bash code builds the dcrwallet binary and places it in `$GOPATH/src/github.com/decred/dcrd`.
Replace `go build` with `go install` to place the binary in `$GOPATH/bin`.
```bash
git clone https://github.com/decred/dcrwallet $GOPATH/src/github.com/decred/dcrwallet
cd $GOPATH/src/github.com/decred/dcrwallet
go install
```

#### dcrstakepool binaries from source
The following bash code builds the dcrstakepool binary and places it in `$GOPATH/src/github.com/decred/dcrstakepool`.
Replace `go build` with `go install` to place the binary in `$GOPATH/bin`.
```bash
git clone https://github.com/decred/dcrstakepool $GOPATH/src/github.com/decred/dcrstakepool
cd $GOPATH/src/github.com/decred/dcrstakepool
GO111MODULE=on go build
```

### Nginx/web server config

- Adapt sample-nginx.conf or setup a different web server in a proxy
  configuration.

## Git Tip Release notes

- The handling of tickets considered invalid because they pay too-low-of-a-fee
  is now integrated directly into dcrstakepool and stakepoold.
  - Users who pass both the adminIPs and the new adminUserIDs checks will see a
    new link on the menu to the new administrative add tickets page.
  - Tickets are added to the MySQL database and then stakepoold is triggered to
    pull an update from the database and reload its config.
  - To accommodate changes to the gRPC API, dcrstakepool/stakepoold had their
    API versions changed to require/advertize 4.0.0. This requires performing
    the upgrade steps outlined below.
- **KNOWN ISSUE** Total tickets count reported by stakepoold may not be totally
  accurate until low fee tickets that have been added to the database can be
  marked as voted.  This will be resolved by future work.
  ([#201](https://github.com/decred/dcrstakepool/issues/201)).

## Git Tip Upgrade Guide

1) Announce maintenance and shut down dcrstakepool.
2) Upgrade Go to the latest stable version if necessary/possible.
3) Perform an upgrade of each stakepoold instance one at a time.
   - Stop stakepoold.
   - Build and restart stakepoold.
4) Edit dcrstakepool.conf and set adminIPs/adminUserIDs appropriately to include
   the administrative staff for whom you wish give the ability to add low fee
   tickets for voting.
5) Upgrade and start dcrstakepool after setting adminUserIDs.
6) Announce maintenance complete after verifying functionality.

## 1.1.1 Release Notes

- dcrd has a new agenda and the vote version in dcrwallet has been
  incremented to v5 on mainnet.
- stakepoold
  - The ticket list is now maintained by doing an initial GetTicket RPC call and
    then subtracts/adds tickets by processing SpentAndMissed/New ticket
    notifications from dcrwallet.  This approach is much faster than the old
    method of calling StakePoolUserInfo for each user.
  - Bug fixes to the above commit and to accommodate changes in dcrwallet.
- Status page
  - StatusUnauthorized error is now thrown rather than a generic one when
    accessing the page as a non-admin.
  - Updated to use new design.
  - Synced dcrwallet walletinfo field list.
- Tickets page
  - Performance was greatly improved by skipping display of historic tickets.
  - Handles users that have only low fee/invalid tickets properly.
  - Expired tickets are now separated from missed.
- General markup improvements.
  - Removed mention of creating a voting account as it has been deprecated.
  - Instructions were further clarified and updated to strongly recommend the
    use of Decrediton/Paymetheus.
  - Fragments of invalid markup were fixed.

## 1.1.1 Upgrade Guide

1) Announce maintenance and shut down dcrstakepool.
2) Perform upgrades on each dcrd+dcrwallet+stakepoold voting cluster one at a
   time.
   - Stop stakepoold, dcrwallet, and dcrd.
   - Upgrade dcrd, dcrwallet to 1.1.0 release binaries or git. If compiling from
   source, Go 1.9 is recommended to pick up improvements to the Golang runtime.
   - Restart dcrd, dcrwallet.
   - Upgrade stakepoold.
   - Start stakepoold.
3) Upgrade and start dcrstakepool.  If you are maintaining a fork, note that
   you need to update the dcrd/chaincfg dependency to a revision that contains
   the new agenda.
4) dcrstakepool will reset the votebits for all users to 1 when it detects the
   new vote version via stakepoold.
5) Announce maintenance complete after verifying functionality.  If possible,
   also announce that a new voting agenda is available and users must login
   to set their preferences for the new agenda.

## Development

If you are modifying templates, sending the USR1 signal to the dcrstakepool
process will trigger a template reload.

### Protoc
In order to regenerate the api.pb.go file, for the gRPC connection with
stakepoold, the following are required:

- libprotoc 3.0.0 (3.4.0 is recommended)
- protoc-gen-go 1.3.0 (1.3.2 is recommended)

## Operations

- dcrstakepool will connect to the database or error out if it cannot do so.

- dcrstakepool will create the stakepool.Users table automatically if it doesn't
  exist.

- dcrstakepool attempts to connect to all of the wallet servers on startup or
  error out if it cannot do so.

- dcrstakepool takes a user's pubkey, validates it, calls getnewaddress on all
  the wallet servers, then createmultisig, and finally importscript.  If any of
  these RPCs fail or returns inconsistent results, the RPC client built-in to
  dcrstakepool will shut down and will not operate until it has been restarted.
  Wallets should be verified to be in sync before restarting.

- User API Tokens have an issuer field set to baseURL from the configuration file.
  Changing the baseURL requires all API Tokens to be re-generated.

## Adding Invalid Tickets

### For Newer versions / git tip

If a user pays an incorrect fee, login as an account that meets the
adminUserIps and adminUserIds restrictions and click the 'Add Low Fee Tickets'
link in the menu.  You will be presented with a list of tickets that are
suitable for adding.  Check the appropriate one(s) and click the submit button.
Upon success, you should see the stakepoold logs reflect that the new tickets
were processed.

### For v1.1.1 and below

If a user pays an incorrect fee you may add their tickets like so (requires dcrd
running with `txindex=1`):

```bash
dcrctl --wallet stakepooluserinfo "MultiSigAddress" | grep -Pzo '(?<="invalid": \[)[^\]]*' | tr -d , | xargs -Itickethash dcrctl --wallet getrawtransaction tickethash | xargs -Itickethex dcrctl --wallet addticket "tickethex"
```

## Backups, monitoring, security considerations

- MySQL should be backed up often and regularly (probably at least hourly).
  Backups should be transferred off-site.  If using binary backups, do a test
  restore. For .sql files, verify visually.

- A monitoring system with alerting should be pointed at dcrstakepool and
  tested/verified to be operating properly.  There is a hidden /status page
  which throws 500 if the RPC client is shutdown.  If your monitoring system
  supports it, add additional points of verification such as: checking that the
  /stats page loads and has expected information in it, create a test account
  and setup automated login testing, etc.

- Wallets should never be used for anything else (they should always have a
  balance of 0).

## Disaster Recovery

**Always keep at least one wallet voting while performing maintenance / restoration!**

- In the case of a total failure of a wallet server:
  - Restore the failed wallet(s) from seed
  - Restart the dcrstakepool process to allow automatic syncing to occur.

## IRC

- irc.freenode.net
- channel #decred

## Issue Tracker

The [integrated github issue tracker](https://github.com/decred/dcrstakepool/issues)
is used for this project.

## License

dcrstakepool is licensed under the [copyfree](http://copyfree.org) MIT/X11 and
ISC Licenses.

## Version History

- 1.1.0 - Architecture change.
  * Per-ticket votebits were removed in favor of per-user voting preferences.
    A voting page was added and the API upgraded to v2 to support getting and
    setting user voting preferences.
  * Addresses from the wallet servers which are needed for generating the 1-of-2
    multisig ticket address are now derived from the new votingwalletextpub
    config option. This removes the need to call getnewaddress on each wallet.
  * An experimental daemon (stakepoold) that votes according to user preference
    is available for testing on testnet. This daemon is not for use on mainnet
    at this time.
- 1.0.0 - Major changes/improvements.
  * API is now at v1 status.  API Tokens are generated for all users with a
    verified email address when upgrading.  Tokens are generated for new
    users on demand when visiting the Settings page which displays their token.
    Authenticated users may use the API to submit a public key address and to
    retrieve ticket purchasing information.  The voting service's stats are also
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
  * SMTPHost now defaults to an empty string so a voting service can be used for
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
