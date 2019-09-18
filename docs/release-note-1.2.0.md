# dcrstakepool v1.2.0

dcrstakepool release 1.2.0 contains all development work completed since
[v1.1.1](https://github.com/decred/dcrstakepool/releases/tag/v1.1.1) (September 2017).
Since then, 20 contributors have produced and merged 160 pull requests.
Changes include a proper interface for handling tickets purchased with insufficient fees,
an overhauled front-end design, security enhancements, updated terminology, reduced
dependencies on third parties, and various bug-fixes.

**Warning:** dcrstakepool v1.2.0 is not compatible with dcrd/dcrwallet v1.4.0.
dcrd and dcrwallet must be built from the current master or v1.5.0 (when available).

## Notable Changes

### Low Fee Ticket Handling

A new admin-only page for handling low-fee tickets has been added to the front-end.
This page will list all tickets which have been purchased with an insufficient fee,
and allow the admin to manually add or remove those tickets from the list of
elligible voting tickets. Previously this operation had to be performed manually
by directly manipulating database values.

### New front-end design

The web front-end for dcrstakepool has been completely overhauled.
The front-end included in release 1.1.1 was fairly basic in style and functionality.
The design was not of a professional standard, and not consistent with other Decred
software such as Decrediton.
Some VSP operators have built their own front-end for dcrstakepool to help them
stand out against their competitors and offer a better experience for their users.
The intent of this update is not only to bring the design in line with other Decred software,
but also to provide all VSP operators an improved and more even starting point for their front-end.

### SMTPS

dcrstakepool depends on an SMTP server to send registration and account recovery emails.
Initial development only supported unencrypted SMTP connections, however various VSP operators
requested support for encrypted SMTPS.
This has been added in release 1.2.0, including support for self-signed certificates.

### Disconnect dcrwallet and dcrstakepool

Initial development of dcrstakepool required a direct connection to both dcrwallet and stakepoold.
This architectural decision was not ideal as it increased complexity, increased the quantity of
RPC calls going over the network, and required additional ports to be opened.
This release severs the connection between dcrstakepool and dcrwallet -
stakepoold is now responsible for **all** interactions with dcrwallet.
As a result, the ports which were previously opened between dcrstakepool and dcrwallet can be closed.

### MySQL storage for HTTP sessions

HTTP sessions in dcrstakepool are implemented using [gorilla/sessions](https://github.com/gorilla/sessions),
which by default uses a file based storage solution for session cookies.
This release replaces that implementation with a custom MySQL session store.
dcrstakepool will create a new table `Session` in the MySQL database if it is not already present.
This was done to address several security issues introduced by the default file storage.

- A user who changes their password or email will now be logged out of all sessions on all devices.
- Cookies stored in a browser can no longer be used to obtain information after a user has logged out.
- A session timeout of six hours has also been added.

### Self-Hosted CAPTCHA

Google's reCAPTCHA has been replaced with a self-hosted solution implemented in go,
[dchest/captcha](https://github.com/dchest/captcha).
All resources required for CAPTCHA are now hosted locally rather than by a third party.
The front-end included in this release executes no external Javascript at all,
granting a significant boost to user security and privacy.

### Security Improvements

- Cross-site Request Forgery (CSRF) protection has been implemented using
  [gorilla/csrf](https://github.com/gorilla/csrf)
- `rel="noopener noreferrer"` has been added to all external hyperlinks to
  ensure no private data is leaked to third parties
- `Cache-Control` HTTP headers are used to prevent sensitive information being
  cached by browsers
- Error page no longer enables embedding malicious links via URL parameters

### Terminology

Various Decred terminology has changed since the last release:

- "Stakepools" are now known as "Voting Service Providers"
- The name Decred should no longer be styled as "deCRED"
- "Paymetheus" wallet is no longer supported and has been superseded by "Decrediton"

## Config Changes

### Removed

These config items **must** be removed from `dcrstakepool.conf`. dcrstakepool will not start if these config items are set.

|Config|PR|Reason|
|------|--|------|
|`recaptchasecret` and `recaptchasitekey`|[#281](https://github.com/decred/dcrstakepool/pull/281)|CAPTCHA is now self-hosted and implemented using [dchest/captcha](https://github.com/dchest/captcha) instead of Google reCAPTCHA.|

### Deprecated

It is **recommended** to remove these config items from `dcrstakepool.conf`. dcrstakepool will ignore these config items and log a warning if they are set. They will be removed completely in the next release.

|Config|PR|Reason|
|------|--|------|
|`wallethosts`, `walletusers`, `walletpasswords` and `walletcerts`|[#470](https://github.com/decred/dcrstakepool/pull/470)|dcrstakepool no longer contacts dcrwallet directly. All comms are now routed through stakepoold.|
|`enablestakepoold`|[#398](https://github.com/decred/dcrstakepool/pull/398)|stakepoold is always required. dcrstakepool cannot function without it.|
|`maxvotedage`|[#402](https://github.com/decred/dcrstakepool/pull/402)|The last N voted tickets are now displayed rather than tickets which voted in the last N days.|
|`minservers`|[#457](https://github.com/decred/dcrstakepool/pull/457)|This value is now hard-coded. Mainnet requires at least two back-end servers, testnet and simnet only need one.|
|`datadir`|[#507](https://github.com/decred/dcrstakepool/pull/507)|This value was unused. dcrstakepool does not write any data to disk.|

### Added

These are new config items added to `dcrstakepool.conf`.

|Config|PR|Reason|
|------|--|------|
|`adminuserids`|[#219](https://github.com/decred/dcrstakepool/pull/219)|Required - enable specific users to access admin functionality|
|`usesmtps`|[#340](https://github.com/decred/dcrstakepool/pull/340)|Optional - enables secure SMTP|
|`smtpskipverify` and `smtpcert`|[#486](https://github.com/decred/dcrstakepool/pull/486)|Optional - configure secure SMTP|
|`maxvotedtickets`|[#457](https://github.com/decred/dcrstakepool/pull/457)|Optional (default 1,000) - restrict how many voted tickets are displayed on the /tickets page.|
|`description` and `designation`|[#339](https://github.com/decred/dcrstakepool/pull/339)|Optional - add a custom designation and description for your VSP|

## Recommended Upgrade Path

1. Build dcrstakepool and stakepoold v1.2.0
1. Build dcrwallet and dcrd from current master or v1.5.0 (when available)
1. Announce maintenance and shut down dcrstakepool
1. Perform an upgrade of each back-end server, one at a time
   1. Stop stakepoold
   1. Stop dcrwallet
   1. Stop dcrd
   1. Install latest dcrd binary and start
   1. Install latest dcrwallet binary and start
   1. Install latest stakepoold binary and start
   1. Check log files for warnings or errors
1. Make required changes to dcrstakepool.conf (detailed [above](#config-changes))
1. Install latest dcrstakepool and start
1. Announce maintenance complete after verifying functionality

## Changelog

### Split dcrstakepool and dcrwallet

- Add ImportScript RPC to stakepoold
([#342](https://github.com/decred/dcrstakepool/pull/342))
- Add ValidateAddress to stakepoold RPC
([#406](https://github.com/decred/dcrstakepool/pull/406))
- Minor style fixes
([#409](https://github.com/decred/dcrstakepool/pull/409))
- Get VoteVersion from stakepoold instead of dcrwallet
([#403](https://github.com/decred/dcrstakepool/pull/403))
- Add StakePoolUserInfo RPC to stakepoold
([#393](https://github.com/decred/dcrstakepool/pull/393))
- introduce stakepoold connection manager
([#381](https://github.com/decred/dcrstakepool/pull/381))
- Move consistency checks and recovery to stakepoold
([#437](https://github.com/decred/dcrstakepool/pull/437))
- rpcserver: Add walletConnected method
([#494](https://github.com/decred/dcrstakepool/pull/494))
- Disconnect dcrstakepool and dcrwallet
([#470](https://github.com/decred/dcrstakepool/pull/470))
- Remove dcrwallet RPC from status page
([#469](https://github.com/decred/dcrstakepool/pull/469))
- multi: getstakeinfo grpc
([#464](https://github.com/decred/dcrstakepool/pull/464))
- Check wallets are all connected before performing write operations
([#463](https://github.com/decred/dcrstakepool/pull/463))
- Autoreconnect
([#510](https://github.com/decred/dcrstakepool/pull/510))
- Prevent unnecessary wallet rescans.
([#519](https://github.com/decred/dcrstakepool/pull/519))

### SQL session storage

- multi: new sql store and session nullification on password change and logout
([#410](https://github.com/decred/dcrstakepool/pull/410))

### Redesign

- Revamp design
([#339](https://github.com/decred/dcrstakepool/pull/339))
- standardise signin/login and signup/register
([#363](https://github.com/decred/dcrstakepool/pull/363))
- allow clicks on any part of row, not just checkbox
([#367](https://github.com/decred/dcrstakepool/pull/367))
- draw notifications on their own row
([#368](https://github.com/decred/dcrstakepool/pull/368))
- Show a message when no tickets to display.
([#369](https://github.com/decred/dcrstakepool/pull/369))
- Add block explorer link for low fee tickets
([#382](https://github.com/decred/dcrstakepool/pull/382))
- tickets: add link to block explorer
([#383](https://github.com/decred/dcrstakepool/pull/383))
- Show ticket info with monospace font.
([#388](https://github.com/decred/dcrstakepool/pull/388))
- Improve pop-up snackbar notifications
([#392](https://github.com/decred/dcrstakepool/pull/392))
- add button to close snackbar notifications.
([#394](https://github.com/decred/dcrstakepool/pull/394))
- show invalid ticket warning.
([#395](https://github.com/decred/dcrstakepool/pull/395))
- prevent horizontal scrolling
([#401](https://github.com/decred/dcrstakepool/pull/401))
- Show last N voted tickets
([#402](https://github.com/decred/dcrstakepool/pull/402))
- tickets page: distinguish between live and immature
([#404](https://github.com/decred/dcrstakepool/pull/404))
- stats page: split stats & status into network stats & pool stats
([#408](https://github.com/decred/dcrstakepool/pull/408))
- gui: fix broken image paths.
([#418](https://github.com/decred/dcrstakepool/pull/418))
- Fix confirmation messages for Registration and password reset
([#419](https://github.com/decred/dcrstakepool/pull/419))
- Remove autorefill in the captcha form
([#421](https://github.com/decred/dcrstakepool/pull/421))
- Notify when not all voted tickets are displayed
([#424](https://github.com/decred/dcrstakepool/pull/424))
- Rework "Connect to Wallet" Page
([#427](https://github.com/decred/dcrstakepool/pull/427))
- show user email address rather than placeholder
([#432](https://github.com/decred/dcrstakepool/pull/432))
- Seperate nginx config for static files
([#433](https://github.com/decred/dcrstakepool/pull/433))
- Remove more dcrwallet rpc calls
([#434](https://github.com/decred/dcrstakepool/pull/434))
- emailupdate: emailupdate to same design as passwordupdate
([#435](https://github.com/decred/dcrstakepool/pull/435))
- Re-add support email address
([#442](https://github.com/decred/dcrstakepool/pull/442))
- main.go: add status to voting page
([#445](https://github.com/decred/dcrstakepool/pull/445))
- views: Fix password reset error popup
([#449](https://github.com/decred/dcrstakepool/pull/449))
- Add CreateMultisig RPC to stakepoold
([#451](https://github.com/decred/dcrstakepool/pull/451))
- Disable captcha form when input is empty.
([#452](https://github.com/decred/dcrstakepool/pull/452))
- Use full size favicon
([#453](https://github.com/decred/dcrstakepool/pull/453))
- Show pool fees on Address page
([#461](https://github.com/decred/dcrstakepool/pull/461))
- Do not attempt to display Invalid ticket height
([#462](https://github.com/decred/dcrstakepool/pull/462))
- controller/main.go: add agenda cache
([#473](https://github.com/decred/dcrstakepool/pull/473))
- Ensure captcha form is centred
([#500](https://github.com/decred/dcrstakepool/pull/500))

### Self-hosted CAPTCHA

- replace recaptcha with self-hosted captcha
([#281](https://github.com/decred/dcrstakepool/pull/281))
- show error message for failed captcha
([#361](https://github.com/decred/dcrstakepool/pull/361))
- Reset captcha value
([#375](https://github.com/decred/dcrstakepool/pull/375))
- Implement client side input validation for captcha
([#456](https://github.com/decred/dcrstakepool/pull/456))

### README

- Add badges to README
([#243](https://github.com/decred/dcrstakepool/pull/243))
- readme: update build instructions for modules.
([#285](https://github.com/decred/dcrstakepool/pull/285))
- Update readme for go 1.12
([#479](https://github.com/decred/dcrstakepool/pull/479))
- new architecture diagram
([#485](https://github.com/decred/dcrstakepool/pull/485))
- README updates to prep for release
([#505](https://github.com/decred/dcrstakepool/pull/505))

### Test Harness

- add tmux test harness
([#476](https://github.com/decred/dcrstakepool/pull/476))
- harness: set dcrd log dir
([#480](https://github.com/decred/dcrstakepool/pull/480))
- Minor harness improvements.
([#497](https://github.com/decred/dcrstakepool/pull/497))
- harness: Write stakepoold logs to file.
([#513](https://github.com/decred/dcrstakepool/pull/513))

### Low Fee Tickets

- multi: improve handling of low fee tickets
([#219](https://github.com/decred/dcrstakepool/pull/219))
- Ensure low fee tickets are detected upon maturation.
([#524](https://github.com/decred/dcrstakepool/pull/524))

### Support SMTPS

- Use goemail
([#298](https://github.com/decred/dcrstakepool/pull/298))
- SMTP should not require user/pass auth
([#306](https://github.com/decred/dcrstakepool/pull/306))
- main: check goemail.NewMessage return value
([#334](https://github.com/decred/dcrstakepool/pull/334))
- email refactoring.
([#340](https://github.com/decred/dcrstakepool/pull/340))
- Encode a url-friendly password when building smtp url
([#454](https://github.com/decred/dcrstakepool/pull/454))
- Add SMTP Cert and SkipVerify configs
([#486](https://github.com/decred/dcrstakepool/pull/486))

### Security

- don't leak whether an email is registered or not
([#230](https://github.com/decred/dcrstakepool/pull/230))
- views: use noopener noreferrer with blank link target
([#292](https://github.com/decred/dcrstakepool/pull/292))
- Adds a Cache-Control header to stop sensitive data from being cached
([#319](https://github.com/decred/dcrstakepool/pull/319))
- Delete password reset tokens on email change
([#323](https://github.com/decred/dcrstakepool/pull/323))
- server: use gorilla for csrf
([#338](https://github.com/decred/dcrstakepool/pull/338))
- Referrer fix
([#377](https://github.com/decred/dcrstakepool/pull/377))
- add noopener noreferrer.
([#384](https://github.com/decred/dcrstakepool/pull/384))
- Remove referrer parameter from error page
([#400](https://github.com/decred/dcrstakepool/pull/400))

### Terminology

- Remove references to Paymetheus
([#244](https://github.com/decred/dcrstakepool/pull/244))
- Replace "deCRED" occurences with "Decred".
([#260](https://github.com/decred/dcrstakepool/pull/260))
- stake pool -> voting service (provider)
([#288](https://github.com/decred/dcrstakepool/pull/288))
- update terminology stakepool => vsp
([#344](https://github.com/decred/dcrstakepool/pull/344))

### Bugfixes & Minor Improvements

- stakepoold: Fix ticket handling to be more accurate.
([#178](https://github.com/decred/dcrstakepool/pull/178))
- FIX: Typo
([#200](https://github.com/decred/dcrstakepool/pull/200))
- handle user with all invalid tickets case properly
([#206](https://github.com/decred/dcrstakepool/pull/206))
- skip fee checks on tickets fetched via GetTickets
([#208](https://github.com/decred/dcrstakepool/pull/208))
- remove voting prioritization
([#212](https://github.com/decred/dcrstakepool/pull/212))
- views: add link to voting site
([#224](https://github.com/decred/dcrstakepool/pull/224))
- config: use cfg.HomeDir to replace defaultHomeDir
([#236](https://github.com/decred/dcrstakepool/pull/236))
- Abort stakepoold startup if cannot get tickets
([#240](https://github.com/decred/dcrstakepool/pull/240))
- stakepoold: warn rather than err when 0 users
([#247](https://github.com/decred/dcrstakepool/pull/247))
- Update config.go
([#270](https://github.com/decred/dcrstakepool/pull/270))
- Do not special case running 1 server.
([#275](https://github.com/decred/dcrstakepool/pull/275))
- Use mainnet.dcrdata.org instead of mainnet.decred.org
([#284](https://github.com/decred/dcrstakepool/pull/284))
- The Sign Up page should not link back to itself.
([#297](https://github.com/decred/dcrstakepool/pull/297))
- admin page: validate ticket list
([#302](https://github.com/decred/dcrstakepool/pull/302))
- controller: Never rescan during script imports
([#307](https://github.com/decred/dcrstakepool/pull/307))
- dont show registration form when pool is closed.
([#389](https://github.com/decred/dcrstakepool/pull/389))
- error on incorrect dcrwallet RPC version
([#425](https://github.com/decred/dcrstakepool/pull/425))
- middleware: bad check
([#428](https://github.com/decred/dcrstakepool/pull/428))
- middleware: don't use middleware when loading images
([#429](https://github.com/decred/dcrstakepool/pull/429))
- Update advice for connecting a new wallet.
([#431](https://github.com/decred/dcrstakepool/pull/431))
- stats.html: add Block Height to stats page
([#440](https://github.com/decred/dcrstakepool/pull/440))
- Add missing bracket to nginx config
([#443](https://github.com/decred/dcrstakepool/pull/443))
- dcrstakepool: correct dcrwallet/stakepoold count check, allow empty smtphost configuration
([#457](https://github.com/decred/dcrstakepool/pull/457))
- stakepooldclient: continue loop on error
([#482](https://github.com/decred/dcrstakepool/pull/482))
- controllers/main.go: tickets rpc error page
([#483](https://github.com/decred/dcrstakepool/pull/483))
- Status page improvements
([#484](https://github.com/decred/dcrstakepool/pull/484))
- controller/main.go: remove enableStakepoold field
([#490](https://github.com/decred/dcrstakepool/pull/490))
- Add missing deprecation notice for minservers
([#506](https://github.com/decred/dcrstakepool/pull/506))
- deprecate unused dcrstakepool datadir config option
([#507](https://github.com/decred/dcrstakepool/pull/507))
- enablevoting=0 in dcrwallet conf
([#520](https://github.com/decred/dcrstakepool/pull/520))
- stakepoold: Stop if wallet voting is enabled
([#523](https://github.com/decred/dcrstakepool/pull/523))

### Tech Debt & Refactoring

- Update DecodeAddress signature
([#197](https://github.com/decred/dcrstakepool/pull/197))
- fix hardcoded table name in addColumn
([#203](https://github.com/decred/dcrstakepool/pull/203))
- Fix some typos
([#222](https://github.com/decred/dcrstakepool/pull/222))
- Remove superfluous assignment
([#241](https://github.com/decred/dcrstakepool/pull/241))
- stakepoold: remove dead code
([#248](https://github.com/decred/dcrstakepool/pull/248))
- self-host d3.min.js
([#283](https://github.com/decred/dcrstakepool/pull/283))
- UserToken type and reworked token-enabled handlers
([#301](https://github.com/decred/dcrstakepool/pull/301))
- multi: more error checking
([#308](https://github.com/decred/dcrstakepool/pull/308))
- Remove duplicated captcha html.
([#337](https://github.com/decred/dcrstakepool/pull/337))
- Tidy up RPC server
([#370](https://github.com/decred/dcrstakepool/pull/370))
- simplify overly complex method
([#371](https://github.com/decred/dcrstakepool/pull/371))
- Remove unused code from dcrclient.go
([#372](https://github.com/decred/dcrstakepool/pull/372))
- Clean Up: runMain() in server.go to return error
([#391](https://github.com/decred/dcrstakepool/pull/391))
- Clean Up: move pubkey parsing to config
([#396](https://github.com/decred/dcrstakepool/pull/396))
- remove enable stake pool as an option, always enabled
([#398](https://github.com/decred/dcrstakepool/pull/398))
- main.go: get for voting page with no multisig redirects to address
([#438](https://github.com/decred/dcrstakepool/pull/438))
- Remove duplicated js-only code
([#439](https://github.com/decred/dcrstakepool/pull/439))
- stakepooldclient: more informative errors
([#481](https://github.com/decred/dcrstakepool/pull/481))
- change NewMainController signature to accept few argument(s)
([#492](https://github.com/decred/dcrstakepool/pull/492))
- multi: cleanup
([#527](https://github.com/decred/dcrstakepool/pull/527))

### Developer-related changes (eg. versioning, travis, dependencies)

- Test against go 1.9 and drop 1.7
([#204](https://github.com/decred/dcrstakepool/pull/204))
- Drop glide, use dep.
([#213](https://github.com/decred/dcrstakepool/pull/213))
- glide up, bump version, release notes
([#215](https://github.com/decred/dcrstakepool/pull/215))
- Multi: update dcrutil and dcrrpcclient imports after move to dcrd
([#220](https://github.com/decred/dcrstakepool/pull/220))
- Enable misspell when running gometalinter
([#242](https://github.com/decred/dcrstakepool/pull/242))
- travis: sync with dcrd's run_tests.sh and fix 1.8
([#245](https://github.com/decred/dcrstakepool/pull/245))
- Bump wallet RPC API version to 5.0.0
([#246](https://github.com/decred/dcrstakepool/pull/246))
- dep: sync
([#251](https://github.com/decred/dcrstakepool/pull/251))
- travis: test against go 1.10.x
([#252](https://github.com/decred/dcrstakepool/pull/252))
- change ticketaddress to ticketbuyer.votingaddress
([#254](https://github.com/decred/dcrstakepool/pull/254))
- Correct licensing
([#258](https://github.com/decred/dcrstakepool/pull/258))
- Sync with latest decred rc1, including testnet3
([#265](https://github.com/decred/dcrstakepool/pull/265))
- Repress logs for unfound ticket lookups.
([#266](https://github.com/decred/dcrstakepool/pull/266))
- Require go 1.11+
([#268](https://github.com/decred/dcrstakepool/pull/268))
- [stakepoold] bump required chain rpc api version
([#271](https://github.com/decred/dcrstakepool/pull/271))
- bump required dcrd JSON RPC version to 5.0.0
([#280](https://github.com/decred/dcrstakepool/pull/280))
- linting and cleanup
([#287](https://github.com/decred/dcrstakepool/pull/287))
- switch to the golangci-lint linter.
([#290](https://github.com/decred/dcrstakepool/pull/290))
- Update decred modules to latest stable versions
([#295](https://github.com/decred/dcrstakepool/pull/295))
- deps: Update to use upstream go-flags.
([#300](https://github.com/decred/dcrstakepool/pull/300))
- Use internal version pkg
([#304](https://github.com/decred/dcrstakepool/pull/304))
- dep: update goemail
([#314](https://github.com/decred/dcrstakepool/pull/314))
- rpc and json dep updates
([#325](https://github.com/decred/dcrstakepool/pull/325))
- sync deps
([#411](https://github.com/decred/dcrstakepool/pull/411))
- go.mod: update to protogen version 3
([#436](https://github.com/decred/dcrstakepool/pull/436))
- rpc: increment api version
([#447](https://github.com/decred/dcrstakepool/pull/447))
- multi: consume v6.0.0 of the dcrd chain server RPC
([#448](https://github.com/decred/dcrstakepool/pull/448))

## Code Contributors

In alphabetical order:
[@C-ollins](https://github.com/C-ollins)
[@chappjc](https://github.com/chappjc)
[@dajohi](https://github.com/dajohi)
[@davecgh](https://github.com/davecgh)
[@degeri](https://github.com/degeri)
[@detailyang](https://github.com/detailyang)
[@dnldd](https://github.com/dnldd)
[@guotie](https://github.com/guotie)
[@hypernoob](https://github.com/hypernoob)
[@infertux](https://github.com/infertux)
[@isuldor](https://github.com/isuldor)
[@itswisdomagain](https://github.com/itswisdomagain)
[@jholdstock](https://github.com/jholdstock)
[@JoeGruffins](https://github.com/JoeGruffins)
[@jolan](https://github.com/jolan)
[@jrick](https://github.com/jrick)
[@mverrilli](https://github.com/mverrilli)
[@pedrotst](https://github.com/pedrotst)
[@teknico](https://github.com/teknico)
[@zhizhongzhiwai](https://github.com/zhizhongzhiwai)
