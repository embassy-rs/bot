#!/bin/bash

cd $(dirname $0)

APP_NAME=embassybot
POSTGRES_IMAGE=postgres:16
DOCKER=${DOCKER:-docker}

${DOCKER} start ${APP_NAME}_postgres > /dev/null 2>&1

export TZ=UTC
export CONFIG_DB_HOST=localhost
export CONFIG_DB_PORT=5432
export CONFIG_DB_NAME=postgres
export CONFIG_DB_USER=postgres
export CONFIG_DB_PASSWORD=postgres

export CONFIG_DB_LOG_QUERIES=true
export CONFIG_DB_LOG_QUERIES_MIN_DURATION=1s
export CONFIG_DB_LOG_TRANSACTIONS=true
export CONFIG_DB_LOG_TRANSACTIONS_MIN_DURATION=1s

export CONFIG_DEBUG=true
export CONFIG_PORTS=web:3000,metrics:3100
export CONFIG_PUBLIC_URL=http://localhost:3000

export CONFIG_GITHUB_APP_ID=$(sed -n 's/^App ID: *//p' secrets.txt)
export CONFIG_GITHUB_WEBHOOK_SECRET=$(sed -n 's/^webhook secret: *//p' secrets.txt)
export CONFIG_GITHUB_PRIVATE_KEY_PATH=$(ls -1 *.private-key.pem | head -1)

# Comment out to actually post comments from a dev instance.
export CONFIG_DRY_RUN=true

# Point a dev instance at real webhooks with a tunnel, e.g.
#   ssh -R 3000:localhost:3000 ...
# and set the app's webhook URL to <tunnel>/webhook.

case "$1" in
    gen)
        find . -regex '.*[\._]gen\.go' -delete
        go run ./internal/sqlbunny gen
        ;;
    genmigrations)
        go run ./internal/sqlbunny migration gen
        ;;
    mergemigrations)
        go run ./internal/sqlbunny migration merge
        ;;
    migrate)
        go run . migrate
        ;;
    run)
        shift
        exec go run . "$@"
        ;;
    serve)
        exec go run github.com/cespare/reflex \
            -r '^(internal/|toolkit/|main.go|d$|secrets.txt$)' \
            -s -- ./d run server
        ;;
    reset)
        ${DOCKER} rm -f ${APP_NAME}_postgres
        ${DOCKER} run -d --name ${APP_NAME}_postgres -e POSTGRES_PASSWORD=postgres --restart=always -p 127.0.0.1:5432:5432 $POSTGRES_IMAGE
        sleep 2
        go run . migrate
        ;;
    sql)
        ${DOCKER} exec -ti ${APP_NAME}_postgres psql -U postgres
        ;;
    *)
        echo "Wrong command, read script source"
        exit 1
esac
