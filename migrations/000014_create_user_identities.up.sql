-- External (federated) identities for platform users.
--
-- Deliberately tenant-independent: an external identity belongs to the person
-- signing in, not to any workspace they happen to be a member of. Nothing in
-- this table grants authorization — roles remain tenant-scoped memberships in
-- tenant_memberships / user_roles, and signing in with Google never writes
-- either of them.
--
-- provider_subject is the provider's stable subject claim (Google's `sub`),
-- never the email address: a Google account's email can change, `sub` cannot,
-- and treating a mutable email as the identity would let an email takeover
-- become an account takeover.
CREATE TABLE user_identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL,
    provider_subject VARCHAR(255) NOT NULL,
    -- The email the provider asserted at link time, kept for support and
    -- audit only. users.email stays the account's authoritative address.
    provider_email VARCHAR(320) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- One provider account resolves to exactly one local user. This is the
    -- constraint that makes repeated Google sign-ins idempotent rather than
    -- duplicate-creating, even under a race between two concurrent callbacks.
    CONSTRAINT user_identities_provider_subject_unique UNIQUE (provider, provider_subject),
    -- ...and one local user holds at most one identity per provider, so a
    -- second Google account cannot be silently attached to an existing user.
    CONSTRAINT user_identities_user_provider_unique UNIQUE (user_id, provider),
    CONSTRAINT user_identities_provider_valid CHECK (provider IN ('GOOGLE'))
);

CREATE INDEX user_identities_user_id_idx ON user_identities (user_id);
