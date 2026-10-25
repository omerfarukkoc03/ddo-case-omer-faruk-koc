-- +goose Up
-- +goose StatementBegin
-- Hayvan sahibi portal hesabı daveti: kimliği klinikte doğrulanan sahip için resepsiyon tek kullanımlık, süreli bir
-- etkinleştirme kodu üretir. Kodun kendisi saklanmaz, yalnız SHA-256 özeti tutulur.
CREATE TABLE portal_invites (
    id          bigserial PRIMARY KEY,
    owner_id    bigint      NOT NULL REFERENCES owners(id),
    email       text        NOT NULL CHECK (position('@' in email) > 1),
    code_hash   bytea       NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz NULL,
    revoked_at  timestamptz NULL,
    created_by  bigint      NULL REFERENCES users(id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
-- Bir sahibin aynı anda en fazla bir kullanılabilir daveti olabilir (yenisi eskisini iptal eder).
CREATE UNIQUE INDEX portal_invites_one_open_uq ON portal_invites (owner_id) WHERE used_at IS NULL AND revoked_at IS NULL;
-- Bir sahip kaydına en fazla bir portal hesabı bağlanır.
CREATE UNIQUE INDEX users_owner_uq ON users (owner_id) WHERE owner_id IS NOT NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS users_owner_uq;
DROP TABLE IF EXISTS portal_invites;
-- +goose StatementEnd
