# Version History

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
