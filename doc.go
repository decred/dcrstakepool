/*
 * Copyright (c) 2013-2016 The Decred developers
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

// dcrstakepool is a rudimentary stake pool that allows voters to pass their
// voting rights over to a third party pool.
//
// Helpful commands for restoring wallets that become desynced:
//
// Dump all redeemscripts from the wallet:
//   dcrctl --wallet listscripts > scripts
//
// Convert a JSON list of scripts to a list of hex redeem scripts:
//   cat allscripts | jq -r '.scripts[].redeemscript' > redeemscrlist
//
// Upload a list of redeem scripts to a wallet:
//   while read p; do
//     dcrctl --wallet importscript $p
//   done < redeemscrlist
//
//
// Instructions for figuring out what address index the wallets should
// be synced to, syncing the address indexes, and then syncing the
// scripts themselves.
//
// 1) Start each redundant wallet with '-d debug' and see what address
//      the address pool is synced to. You may need to getnewaddress to
//      see reveal the address after the current one in the debug output.
// 2) Restore from seed and run this script (if you need to increment
//      the change addresses too, run again with getrawchangeaddress
//      instead of getnewaddress) where ??? == the address index to
//      restore to:
//
//      for i in `seq 1 ???`; do
//        dcrctl --wallet getnewaddress
//      done
//
// 3) Upload the redeem scripts after dumping them from the old wallet
//      as detailed above. The blockchain will automatically resync itself
//      and detect all tickets, votes, and revocations. This may take a
//      while.

package main
