-- name: GetReadyUpstreamByChainIdSource :one
SELECT * FROM ready_upstream 
WHERE chain_id = ? AND source = ?;

-- name: ListReadyUpstreamsByChainId :many
SELECT * FROM ready_upstream 
WHERE chain_id = ?;

-- name: CreateReadyUpstream :execresult
INSERT INTO ready_upstream (
  chain_id, source, rpc, created, updated
) VALUES (
  ?, ?, ?, ?, ?
);

-- name: UpdateReadyUpstreamRpc :execresult
UPDATE ready_upstream SET rpc = ?, updated = ? 
WHERE chain_id = ? AND source = ?;

-- name: GetSecretKeyByAccessKey :one
SELECT * FROM secret_key 
WHERE access_key = ?;

-- name: CreateSecretKey :execresult
INSERT INTO secret_key (
  access_key, secret_key, `service`, `group`, allow_origins, allow_ips, route_rules, created, updated
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?
);

-- name: UpdateSecretKey :execresult
UPDATE secret_key SET `group` = ?, `service` = ?, allow_origins = ?, allow_ips = ?, route_rules = ?, updated = ?
WHERE access_key = ?;

-- name: ListSecretKeys :many
SELECT * FROM secret_key;

-- name: GetConfigByKey :one
SELECT * FROM config 
WHERE `key` = ? AND module = ?;

-- name: CreateConfig :execresult
INSERT INTO config (
  `key`, `value`, module, created, updated
) VALUES (
  ?, ?, ?, ?, ?
);

-- name: UpdateConfigValue :execresult
UPDATE config SET `value` = ?, updated = ? 
WHERE `key` = ? AND module = ?;
