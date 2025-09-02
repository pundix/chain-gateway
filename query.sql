-- name: GetReadyUpstreamByChainIdSource :one
SELECT * FROM ready_upstream 
WHERE chain_id = ? AND source = ? LIMIT 1;

-- name: ListReadyUpstreamsByChainIdSourceNotEq :many
SELECT * FROM ready_upstream 
WHERE chain_id = ? AND source != ?;

-- name: ListReadyUpstreamsByChainIdSource :many
SELECT * FROM ready_upstream 
WHERE chain_id = ? AND source = ?;

-- name: ListReadyUpstreamsBySource :many
SELECT * FROM ready_upstream 
WHERE source = ?;

-- name: GetSecretKeyByAccessKey :one
SELECT * FROM secret_key 
WHERE access_key = ?;

-- name: CreateSecretKey :execresult
INSERT INTO secret_key (
  access_key, secret_key, `service`, `group`, created_at
) VALUES (
  ?, ?, ?, ?, ?
);

-- name: ListSecretKeys :many
SELECT * FROM secret_key;

-- name: GetKvCacheByKey :one
SELECT * FROM kv_cache 
WHERE `key` = ? LIMIT 1;

-- name: CreateKvCache :execresult
INSERT INTO kv_cache (
  `key`, `value`, created_at
) VALUES (
  ?, ?, ?
);

-- name: UpdateKvCacheValue :execresult
UPDATE kv_cache SET `value` = ?, created_at = ? 
WHERE `key` = ?;

-- name: ListUpstreamsBySourceNotEq :many
SELECT * FROM upstream 
WHERE source != ?;

-- name: ListCheckRulesBySourceNotEq :many
SELECT * FROM check_rule 
WHERE source != ?;

-- name: ListCheckRulesBySource :many
SELECT * FROM check_rule 
WHERE source = ?;

-- name: ListCheckRulesByChainIdSource :many
SELECT * FROM check_rule 
WHERE chain_id =? AND source = ?;

-- name: CreateCheckRule :execresult
INSERT INTO check_rule (
  chain_id, source, rules, created_at
) VALUES (
  ?, ?, ?, ?
);

-- name: UpdateCheckRule :execresult
UPDATE check_rule SET rules = ?, created_at = ? 
WHERE chain_id = ? AND source = ?;

-- name: ListUpstreams :many
SELECT * FROM upstream;

-- name: DelUpstreamBySource :exec
DELETE FROM upstream 
WHERE source = ?;

-- name: ListUpstreamsByChainIdSource :many
SELECT * FROM upstream 
WHERE chain_id = ? AND source = ?;

-- name: DelReadyUpstreamBySource :exec
DELETE FROM ready_upstream 
WHERE source = ?;

-- name: ListUpstreamsBySource :many
SELECT * FROM upstream 
WHERE source = ?;

-- name: CreateUpstream :execresult
INSERT INTO upstream (
  chain_id, source, rpc, created_at
) VALUES (
  ?, ?, ?, ?
);

-- name: UpdateUpstreamRpc :execresult
UPDATE upstream SET rpc = ?, created_at = ? 
WHERE chain_id = ? AND source = ?;

-- name: DelUpstreamByChainIdSource :exec
DELETE FROM upstream 
WHERE chain_id = ? AND source = ?;

-- name: DelReadyUpstreamByChainIdSource :exec
DELETE FROM ready_upstream 
WHERE chain_id = ? AND source = ?;

-- name: CreateReadyUpstream :execresult
INSERT INTO ready_upstream (
  chain_id, source, rpc, created_at
) VALUES (
  ?, ?, ?, ?
);

-- name: UpdateReadyUpstreamRpc :execresult
UPDATE ready_upstream SET rpc = ?, created_at = ? 
WHERE chain_id = ? AND source = ?;

-- name: ListUpstreamsInChainIdsAndSourceNotEq :many
SELECT * FROM upstream 
WHERE chain_id IN (sqlc.slice('chainIds')) AND source!=?;

-- name: ListCheckRulesInChainIdsAndSourceNotEq :many
SELECT * FROM check_rule 
WHERE chain_id IN (sqlc.slice('chainIds')) AND source!=?;

-- name: ListKvCacheInKeys :many
SELECT * FROM kv_cache 
WHERE `key` IN (sqlc.slice('keys'));