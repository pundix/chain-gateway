CREATE TABLE IF NOT EXISTS ready_upstream (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chain_id TEXT NOT NULL,
    source TEXT NOT NULL,
    rpc TEXT NOT NULL,
    created BIGINT NOT NULL,
    updated BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ready_upstream_on_chain_id ON ready_upstream (chain_id);

CREATE TABLE IF NOT EXISTS secret_key (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    `service` TEXT NOT NULL,
    `group` TEXT NOT NULL,
    allow_origins TEXT NOT NULL DEFAULT '',
    allow_ips TEXT NOT NULL DEFAULT '',
    route_rules TEXT NOT NULL DEFAULT '',
    created BIGINT NOT NULL,
    updated BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_secret_key_on_access_key ON secret_key (access_key);

CREATE TABLE IF NOT EXISTS config (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    `key` TEXT NOT NULL,
    `value` TEXT NOT NULL,
    module TEXT NOT NULL,
    created BIGINT NOT NULL,
    updated BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_config_on_key ON config (`key`);