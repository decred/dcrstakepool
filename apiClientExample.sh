#!/bin/sh
# This script registers a new account at a stakepool, waits a bit
# for the email verification link to be clicked, submits an address,
# requests the ticket purchasing information for the account, and
# then requests the pool's statistics.

apiURL="http://127.0.0.1:8000/api/v0.1"
cookieFile=$(mktemp /tmp/stakepoolapiclient.XXXXXXXXXX)
email="example@example.com"
password="blake256"
pubKeyAddr="TkKnJTAQTQ42nAwLj9wSXJcrKadSJneNkXT9LW25kqBKH646N4Bws"
secsForEmailVerification=15

apiCmd() {
    # $1 = cmd, $2 = data
    if [ -z "$2" ]; then
        echo "running curl -s -b $cookieFile -c $cookieFile $apiURL/$1"
        r="$(curl -s -b $cookieFile -c $cookieFile $apiURL/$1)"
    else
        updateCsrfToken
        echo "running curl -s -b $cookieFile -c $cookieFile --data $2&csrf_token=$csrftoken $apiURL/$1"
        r="$(curl -s -b $cookieFile -c $cookieFile --data "$2&csrf_token=$csrftoken" $apiURL/$1)"
    fi

    if [ -z "$r" ]; then
        fatal "command $1 failed"
    fi

    status=$(echo $r |jq -r .status)

    if [ "$status" == "error" ]; then
        echo "WARNING: command returned with error"
    fi
    # can add additional processing of the result here
    echo $r
}

cleanUp() {
    echo "removing $cookieFile"
    rm $cookieFile
}

fatal() {
    echo >&2 "$1"
    cleanUp
    exit 1
}

updateCsrfToken() {
    csrftoken=$(grep XSRF-TOKEN $cookieFile |awk '{print $7}')
}

waitForVerification() {
    echo "sleeping $secsForEmailVerification seconds to allow email verification link to be clicked"
    for i in `seq $secsForEmailVerification -1 1`;
        do echo "$i seconds left to verify email"; sleep 1
    done
}

command -v jq >/dev/null 2>&1 || { fatal "I require the binary jq (https://stedolan.github.io/jq/) but it's not installed.  Aborting."; }

echo "using cookieFile $cookieFile"

apiCmd "startsession"

apiCmd "signup" "email=$email&password=$password&passwordrepeat=$password"

waitForVerification

apiCmd "signin" "email=$email&password=$password"

apiCmd "address" "UserPubKeyAddr=$pubKeyAddr"

apiCmd "getPurchaseInfo"

apiCmd "stats"

cleanUp
