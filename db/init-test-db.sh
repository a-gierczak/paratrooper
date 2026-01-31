#!/usr/bin/env sh

set -e

SCHEMA=$(cat /tmp/schema.sql)
SEED=$(cat /tmp/test-db-seed.sql)

# Using a shell init script to be able to use environment variable substitution
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
  ${SCHEMA}
  ${SEED}
EOSQL
