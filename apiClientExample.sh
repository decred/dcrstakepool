#!/bin/sh
# This script registers a new account at a stakepool, waits a bit
# for the email verification link to be clicked, submits an address,
# requests the ticket purchasing information for the account, and
# then requests the pool's statistics.

apiKEY="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpYXQiOjE0ODE3MzAzNjYsImlzcyI6Imh0dHA6Ly8xMjcuMC4wLjE6ODAwMCIsImxvZ2dlZEluQXMiOjY0fQ.DS49iqN3hFjAqwTnUKIbJ-Mg3sdwKUVE_diwp7dTok4"
apiURL="http://127.0.0.1:8000/api/v1"
#cookieFile=$(mktemp /tmp/stakepoolapiclient.XXXXXXXXXX)
email="example@example.com"
password="blake256"
pubKeyAddr="TkKnJTAQTQ42nAwLj9wSXJcrKadSJneNkXT9LW25kqBKH646N4Bws"
secsForEmailVerification=15

apiCmd() {
    # $1 = cmd, $2 = data
    if [ -z "$2" ]; then
        echo "running curl -s -H \"Authorization: Bearer $apiKEY\" $apiURL/$1"
        r=$(curl -s -H "Authorization: Bearer $apiKEY" $apiURL/$1)
    else
        echo "running curl -s -H \"Authorization: Bearer $apiKEY\" --data $2 $apiURL/$1"
        r=$(curl -s -H "Authorization: Bearer $apiKEY" --data "$2" $apiURL/$1)
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
    #cleanUp
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

#echo "using cookieFile $cookieFile"

#apiCmd "startsession"

#apiCmd "signup" "email=$email&password=$password&passwordrepeat=$password"

#waitForVerification

#apiCmd "signin" "email=$email&password=$password"

apiCmd "address" "UserPubKeyAddr=$pubKeyAddr"

apiCmd "getpurchaseinfo"

apiCmd "stats"

#cleanUp
