# 1.1.1 Release Notes

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