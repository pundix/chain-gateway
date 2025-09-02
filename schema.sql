CREATE TABLE IF NOT EXISTS upstream (
    id BIGINT PRIMARY KEY,
    chain_id TEXT NOT NULL,
    source TEXT NOT NULL,
    rpc TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_upstream_on_chain_id ON upstream (chain_id);

CREATE TABLE IF NOT EXISTS ready_upstream (
    id BIGINT PRIMARY KEY,
    chain_id TEXT NOT NULL,
    source TEXT NOT NULL,
    rpc TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ready_upstream_on_chain_id ON ready_upstream (chain_id);

CREATE TABLE IF NOT EXISTS secret_key (
    id BIGINT PRIMARY KEY,
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    `service` TEXT NOT NULL,
    `group` TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_secret_key_on_access_key ON secret_key (access_key);

CREATE TABLE IF NOT EXISTS check_rule (
    id BIGINT PRIMARY KEY,
    chain_id TEXT NOT NULL,
    source TEXT NOT NULL,
    rules TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_check_rule_on_chain_id ON check_rule (chain_id);


CREATE TABLE IF NOT EXISTS kv_cache (
    id BIGINT PRIMARY KEY,
    `key` TEXT NOT NULL,
    `value` TEXT NOT NULL,
    created_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kv_cache_on_key ON kv_cache (`key`);

ALTER TABLE secret_key ADD COLUMN allow_origins TEXT NOT NULL DEFAULT '';
ALTER TABLE secret_key ADD COLUMN allow_ips TEXT NOT NULL DEFAULT '';
