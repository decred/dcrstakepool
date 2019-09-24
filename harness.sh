#!/bin/bash

# To use this test harness simply run `./harness.sh` from the repo root.
#
# After writing the config files and starting all of the processes, the web
# interface should become available almost immediately. It will take a short
# while for dcrstakepool to become fully functional because the wallets need
# to sync and complete a re-scan before they can be used.
#
# This harness makes a few assumptions
#
# - tmux is installed
# - dcrd, dcrwallet, stakepoold and dcrstakepool are available on $PATH
# - Decred testnet chain is already downloaded and sync'd
# - MySQL is configured at 127.0.0.1:3306
# - Database `stakepool` and user `stakepool` with password `password` exist
# - The following files exist:
#   - ${HOME}/.dcrd/rpc.cert
#   - ${HOME}/.dcrd/rpc.key
#   - ${HOME}/.dcrwallet/rpc.cert
#   - ${HOME}/.dcrwallet/rpc.key
#   - ${HOME}/.stakepoold/rpc.cert

set -e

SESSION="harness"
NODES_ROOT=~/harness
RPC_USER="user"
RPC_PASS="pass"
NUMBER_OF_BACKENDS=2

DCRD_RPC_CERT="${HOME}/.dcrd/rpc.cert"
DCRD_RPC_KEY="${HOME}/.dcrd/rpc.key"
DCRD_RPC_LISTEN="127.0.0.1:12321"

WALLET_PASS="12345"
WALLET_RPC_CERT="${HOME}/.dcrwallet/rpc.cert"
WALLET_RPC_KEY="${HOME}/.dcrwallet/rpc.key"

STAKEPOOLD_RPC_CERT="${HOME}/.stakepoold/rpc.cert"

MYSQL_HOST="127.0.0.1"
MYSQL_PORT="3306"
MYSQL_DB="stakepool"
MYSQL_USER="stakepool"
MYSQL_PASS="password"

DCRSTAKEPOOL_ADMIN_IPS="127.0.0.1,::1"
DCRSTAKEPOOL_ADMIN_IDS="1,6,46"
DCRSTAKEPOOL_SMTP_FROM="admin@vsp.com"
DCRSTAKEPOOL_SMTP_HOST="localhost:2500"

VOTING_WALLET_SEED="c539a410d13ce16dced00ed54d2644aa79302e9853bb2cd6c7d9520bf0546da27"
VOTING_WALLET_DEFAULT_ACCT_PUB_KEY="tpubVpa7jQBLn1RH1dtbNTQoWaxnzmqedpQX8ZxUoUfjMbNw3CYapSZMikw9ktFvhmb5Xwjpz2Uiin9Zncaw14cHq6oZH69Uws4yCZkZdKip9vZ"
COLD_WALLET_PUB_KEY="tpubVpksTuUYrAgbfwZvU3h9gPLPemf6WCisSrEtYS7gKWqRdPKeqd9t8wPxX99ubvm82N18JeFhhK357q5PuJsVj1qnC7MFVjS37dMjzH7SD34"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

tmux new-session -d -s $SESSION

#################################################
# Setup the dcrd node.
#################################################

tmux rename-window -t $SESSION 'dcrd'

echo "Writing config for testnet dcrd node"
mkdir -p "${NODES_ROOT}/dcrd"
cp "${DCRD_RPC_CERT}" "${NODES_ROOT}/dcrd/rpc.cert"
cp "${DCRD_RPC_KEY}"  "${NODES_ROOT}/dcrd/rpc.key"
cat > "${NODES_ROOT}/dcrd/dcrd.conf" <<EOF
rpcuser=${RPC_USER}
rpcpass=${RPC_PASS}
rpccert=${NODES_ROOT}/dcrd/rpc.cert
rpckey=${NODES_ROOT}/dcrd/rpc.key
rpclisten=${DCRD_RPC_LISTEN}
testnet=true
logdir=${NODES_ROOT}/master/log
EOF

echo "Starting dcrd node"
tmux send-keys "dcrd -C ${NODES_ROOT}/dcrd/dcrd.conf" C-m 

sleep 3 # Give dcrd time to start

#################################################
# Setup multiple back-ends.
#################################################

for ((i = 1; i <= $NUMBER_OF_BACKENDS; i++)); do
    WALLET_RPC_LISTEN="127.0.0.1:2011${i}"
    
    STAKEPOOLD_RPC_LISTEN="127.0.0.1:3010$i"
    ALL_STAKEPOOLDS="${ALL_STAKEPOOLDS:+$ALL_STAKEPOOLDS,}${STAKEPOOLD_RPC_LISTEN}"
    ALL_STAKEPOOLD_RPC_CERTS="${ALL_STAKEPOOLD_RPC_CERTS:+$ALL_STAKEPOOLD_RPC_CERTS,}${STAKEPOOLD_RPC_CERT}"

    #################################################
    # dcrwallet
    #################################################
    echo ""
    echo "Writing config for dcrwallet-${i}"
    mkdir -p "${NODES_ROOT}/dcrwallet-${i}"
    cp "${WALLET_RPC_CERT}" "${NODES_ROOT}/dcrwallet-${i}/rpc.cert"
    cp "${WALLET_RPC_KEY}"  "${NODES_ROOT}/dcrwallet-${i}/rpc.key"
    cat > "${NODES_ROOT}/dcrwallet-${i}/dcrwallet.conf" <<EOF
username=${RPC_USER}
password=${RPC_PASS}
rpccert=${NODES_ROOT}/dcrwallet-${i}/rpc.cert
rpckey=${NODES_ROOT}/dcrwallet-${i}/rpc.key
logdir=${NODES_ROOT}/dcrwallet-${i}/log
appdata=${NODES_ROOT}/dcrwallet-${i}
testnet=true
pass=${WALLET_PASS}
rpcconnect=${DCRD_RPC_LISTEN}
grpclisten=127.0.0.1:2010${i}
rpclisten=${WALLET_RPC_LISTEN}
stakepoolcoldextkey=${COLD_WALLET_PUB_KEY}:10000
EOF

    echo "Starting dcrwallet-${i}"
    tmux new-window -t $SESSION -n "dcrwallet-${i}"
    tmux send-keys "dcrwallet -C ${NODES_ROOT}/dcrwallet-${i}/dcrwallet.conf --create" C-m
    sleep 2
    tmux send-keys "${WALLET_PASS}" C-m "${WALLET_PASS}" C-m "n" C-m "y" C-m
    sleep 2
    tmux send-keys "${VOTING_WALLET_SEED}" C-m C-m
    tmux send-keys "dcrwallet -C ${NODES_ROOT}/dcrwallet-${i}/dcrwallet.conf " C-m
    sleep 12 # Give dcrwallet time to start

    #################################################
    # stakepoold
    #################################################

    echo ""
    echo "Writing config for stakepoold-${i}"
    mkdir -p "${NODES_ROOT}/stakepoold-${i}"
    cat > "${NODES_ROOT}/stakepoold-${i}/stakepoold.conf" <<EOF
dcrdhost=${DCRD_RPC_LISTEN}
dcrdcert=${DCRD_RPC_CERT}
dcrduser=${RPC_USER}
dcrdpassword=${RPC_PASS}
dbhost=${MYSQL_HOST}
dbport=${MYSQL_PORT}
logdir=${NODES_ROOT}/stakepoold-${i}/log
dbname=${MYSQL_DB}
dbuser=${MYSQL_USER}
dbpassword=${MYSQL_PASS}
coldwalletextpub=${COLD_WALLET_PUB_KEY}
wallethost=${WALLET_RPC_LISTEN}
walletcert=${WALLET_RPC_CERT}
walletuser=${RPC_USER}
walletpassword=${RPC_PASS}
testnet=true
appdata=${NODES_ROOT}/stakepoold-${i}
rpclisten=${STAKEPOOLD_RPC_LISTEN}
EOF

    echo "Starting stakepoold-${i}"
    tmux new-window -t $SESSION -n "stakepoold-${i}"
    tmux send-keys "stakepoold -C ${NODES_ROOT}/stakepoold-${i}/stakepoold.conf " C-m
done

#################################################
# Setup dcrstakepool
#################################################
echo ""
echo "Writing config for dcrstakepool"
mkdir -p "${NODES_ROOT}/dcrstakepool"
cat > "${NODES_ROOT}/dcrstakepool/dcrstakepool.conf" <<EOF
logdir=${NODES_ROOT}/dcrstakepool/log
votingwalletextpub=${VOTING_WALLET_DEFAULT_ACCT_PUB_KEY}
apisecret=not_very_secret_at_all
cookiesecret=not_very_secret_at_all
dbhost=${MYSQL_HOST}
dbport=${MYSQL_PORT}
dbname=${MYSQL_DB}
dbuser=${MYSQL_USER}
dbpassword=${MYSQL_PASS}
coldwalletextpub=${COLD_WALLET_PUB_KEY}
testnet=true
smtphost=${DCRSTAKEPOOL_SMTP_HOST}
smtpfrom=${DCRSTAKEPOOL_SMTP_FROM}
adminips=${DCRSTAKEPOOL_ADMIN_IPS}
adminuserids=${DCRSTAKEPOOL_ADMIN_IDS}
stakepooldhosts=${ALL_STAKEPOOLDS}
stakepooldcerts=${ALL_STAKEPOOLD_RPC_CERTS}
EOF

tmux new-window -t $SESSION -n "dcrstakepool"

echo "Starting dcrstakepool"
sleep 10 # Give stakepoold time to start
tmux send-keys "dcrstakepool -C ${NODES_ROOT}/dcrstakepool/dcrstakepool.conf" C-m 
sleep 2

#################################################
# All done - attach to tmux session.
#################################################

tmux attach-session -t $SESSION
