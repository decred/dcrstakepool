# dcrstakepool v1.5.0

dcrstakepool release 1.5.0 contains all development work completed since
[v1.2.0](https://github.com/decred/dcrstakepool/releases/tag/v1.2.0) (September 2019).
Since then, 7 contributors have produced and merged 37 pull requests.
Changes include minor GUI enhancements, improved HTTP request logging,
some streamlining of existing code, and various bug-fixes.

dcrstakepool 1.5.0 requires dcrd/dcrwallet 1.5.0 or later.

## Config Changes

### Removed

All of the following config items were removed by [#546](https://github.com/decred/dcrstakepool/pull/546) as they were deprecated in the previous dcrstakepool release.
These config items **must** be removed from `dcrstakepool.conf`.
dcrstakepool will not start if these config items are set.

|Config|Reason|
|------|------|
|`wallethosts`, `walletusers`, `walletpasswords` and `walletcerts`|dcrstakepool no longer contacts dcrwallet directly. All comms are now routed through stakepoold.|
|`enablestakepoold`|stakepoold is always required. dcrstakepool cannot function without it.|
|`maxvotedage`|The last N voted tickets are now displayed rather than tickets which voted in the last N days.|
|`minservers`|This value is now hard-coded. Mainnet requires at least two back-end servers, testnet and simnet only need one.|
|`datadir`|This value was unused. dcrstakepool does not write any data to disk.|

### Deprecated

It is **recommended** to remove these config items from `dcrstakepool.conf` **and** `stakepoold.conf`.
dcrstakepool and stakepoold will ignore these config items and log a warning if they are set.
They will be removed completely in the next release.

|Config|PR|Reason|
|------|--|------|
|`Profile`, `CPUProfile` and `memprofile`|[#545](https://github.com/decred/dcrstakepool/pull/545)|These config items were not used at all by dcrstakepool|

### Added

No new config items have been added in 1.5.0.

## Recommended Upgrade Path

1. Build dcrstakepool and stakepoold v1.5.0
1. Build dcrwallet and dcrd v1.5.0
1. Announce maintenance and shut down dcrstakepool
1. Perform an upgrade of each back-end server, one at a time
   1. Stop stakepoold
   1. Stop dcrwallet
   1. Stop dcrd
   1. Install latest dcrd binary and start
   1. Install latest dcrwallet binary and start
   1. Make required changes to stakepoold.conf (detailed [above](#config-changes))
   1. Ensure both dcrd and dcrwallet are synced with the network
   1. Install latest stakepoold binary and start
   1. Check log files for warnings or errors
1. Make required changes to dcrstakepool.conf (detailed [above](#config-changes))
1. Install latest dcrstakepool and start
1. Announce maintenance complete after verifying functionality

## Changelog

### README

- readme: Simplify and update information
([#575](https://github.com/decred/dcrstakepool/pull/575))

### Test Harness

- harness: add stakepoolcoldextkey to dcrwallet.conf
([#536](https://github.com/decred/dcrstakepool/pull/536))

### GUI

- input new styling
([#491](https://github.com/decred/dcrstakepool/pull/491))
- Update to d3.js version 5.12.0
([#540](https://github.com/decred/dcrstakepool/pull/540))
- Always show vote options on /voting
([#583](https://github.com/decred/dcrstakepool/pull/583))
- Update Profit => Reward
([#584](https://github.com/decred/dcrstakepool/pull/584))

### Config

- Compare ColdWalletExtPub config value across dcrstakepool and all stakepoold configs
([#526](https://github.com/decred/dcrstakepool/pull/526))
- Deprecate unused Profile configs.
([#545](https://github.com/decred/dcrstakepool/pull/545))
- Remove config items deprecated in 1.2
([#546](https://github.com/decred/dcrstakepool/pull/546))

### nginx config

- Add zipassets.sh to facilitate gzip_static use
([#537](https://github.com/decred/dcrstakepool/pull/537))
- update sample-nginx.conf
([#538](https://github.com/decred/dcrstakepool/pull/538))
- Note version requirement for limit_req delay param
([#580](https://github.com/decred/dcrstakepool/pull/580))

### Bugfixes & Minor Improvements

- Define and use a custom HTTP request logger
([#535](https://github.com/decred/dcrstakepool/pull/535))
- Prevent header links from moving
([#559](https://github.com/decred/dcrstakepool/pull/559))

### Tech Debt & Refactoring

- multi: Update maxuser TODOs
([#466](https://github.com/decred/dcrstakepool/pull/466))
- stakepoold: Add shutdown context
([#528](https://github.com/decred/dcrstakepool/pull/528))
- stakepoold: context -> stakepool
([#529](https://github.com/decred/dcrstakepool/pull/529))
- clean up LoadTemplates and link to explorer on stats page
([#534](https://github.com/decred/dcrstakepool/pull/534))
- dcrstakepool: Add shutdown context
([#539](https://github.com/decred/dcrstakepool/pull/539))
- Simplify construction of dcrdata urls
([#544](https://github.com/decred/dcrstakepool/pull/544))
- stakepoold: Move rpc/rpcserver to rpc/server
([#550](https://github.com/decred/dcrstakepool/pull/550))
- params: Remove unused WalletRPCServerPort
([#553](https://github.com/decred/dcrstakepool/pull/553))
- Remove all in-line JavaScript
([#560](https://github.com/decred/dcrstakepool/pull/560))
- Move page specific code out of global.js
([#561](https://github.com/decred/dcrstakepool/pull/561))
- Report card
([#566](https://github.com/decred/dcrstakepool/pull/566))

### Developer-related changes (eg. versioning, travis, dependencies)

- travis: test go1.13
([#531](https://github.com/decred/dcrstakepool/pull/531))
- build: replace travis-ci with ci via github actions
([#541](https://github.com/decred/dcrstakepool/pull/541))
- ci: only run on pull requests
([#543](https://github.com/decred/dcrstakepool/pull/543))
- build: re-add push action
([#547](https://github.com/decred/dcrstakepool/pull/547))
- Remove gorilla/context and update deps.
([#549](https://github.com/decred/dcrstakepool/pull/549))
- multi: Update modules
([#554](https://github.com/decred/dcrstakepool/pull/554))
- bump version to 1.5.0-pre.
([#558](https://github.com/decred/dcrstakepool/pull/558))
- modules: sync to latest
([#565](https://github.com/decred/dcrstakepool/pull/565))
- multi: drop udb usage
([#567](https://github.com/decred/dcrstakepool/pull/567))
- build: upgrade golangci-lint
([#570](https://github.com/decred/dcrstakepool/pull/570))
- build: upgrade deps
([#576](https://github.com/decred/dcrstakepool/pull/576))
- Prepare v1.5.0
([#578](https://github.com/decred/dcrstakepool/pull/578))

## Code Contributors

In alphabetical order:
[@amassarwi](https://github.com/amassarwi)
[@chappjc](https://github.com/chappjc)
[@dajohi](https://github.com/dajohi)
[@isuldor](https://github.com/isuldor)
[@itswisdomagain](https://github.com/itswisdomagain)
[@jholdstock](https://github.com/jholdstock)
[@JoeGruffins](https://github.com/JoeGruffins)
