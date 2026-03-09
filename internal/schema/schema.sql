-- internal/schema/schema.sql
-- THE SOURCE OF TRUTH.
--
-- Rules:
--   1. This is the ONLY file that describes your database structure.
--   2. Never write ALTER TABLE by hand. Edit this file and run `make gen`.
--   3. Never run ORM AutoMigrate in production. Use `make migrate-up`.
--   4. Compatible with: sqlc, SQLBoiler, Bob, pgx, GORM (read-only), Bun, Beego.


CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE "users" (
    "id"           bigserial    NOT NULL,
    "uuid"         uuid         NOT NULL DEFAULT gen_random_uuid(),
    "email"        varchar(255) NOT NULL,
    "password_hash text         NOT NULL,
    "role"         varchar(50)  NOT NULL DEFAULT 'user',
    "is_active"    boolean      NOT NULL DEFAULT true,
    "created_at"   timestamptz  NOT NULL DEFAULT now(),
    "updated_at"   timestamptz  NOT NULL DEFAULT now(),
    "deleted_at"   timestamptz,                           -- soft-delete support
    PRIMARY KEY ("id"),
    UNIQUE ("uuid"),
    UNIQUE ("email")
);

CREATE INDEX "idx_users_email"      ON "users" USING btree ("email");
CREATE INDEX "idx_users_deleted_at" ON "users" USING btree ("deleted_at");

CREATE TABLE "posts" (
    "id"         bigserial    NOT NULL,
    "user_id"    bigint       NOT NULL,
    "title"      varchar(500) NOT NULL,
    "body"       text,
    "status"     varchar(20)  NOT NULL DEFAULT 'draft',
    "created_at" timestamptz  NOT NULL DEFAULT now(),
    "updated_at" timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY ("id"),
    CONSTRAINT "fk_posts_user"
        FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "idx_posts_user_id"   ON "posts" USING btree ("user_id");
CREATE INDEX "idx_posts_status"    ON "posts" USING btree ("status");
CREATE INDEX "idx_posts_created"   ON "posts" USING btree ("created_at" DESC);

CREATE TABLE "tags" (
    "id"   bigserial    NOT NULL,
    "name" varchar(100) NOT NULL,
    PRIMARY KEY ("id"),
    UNIQUE ("name")
);

CREATE TABLE "post_tags" (
    "post_id" bigint NOT NULL,
    "tag_id"  bigint NOT NULL,
    PRIMARY KEY ("post_id", "tag_id"),
    CONSTRAINT "fk_post_tags_post"
        FOREIGN KEY ("post_id") REFERENCES "posts" ("id") ON DELETE CASCADE,
    CONSTRAINT "fk_post_tags_tag"
        FOREIGN KEY ("tag_id")  REFERENCES "tags" ("id")  ON DELETE CASCADE
);

CREATE TABLE "audit_log" (
    "id"         bigserial   NOT NULL,
    "user_id"    bigint,                -- nullable: system actions have no user
    "action"     varchar(100) NOT NULL,
    "table_name" varchar(100) NOT NULL,
    "row_id"     bigint,
    "payload"    jsonb,
    "created_at" timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY ("id")
);

CREATE INDEX "idx_audit_user"  ON "audit_log" USING btree ("user_id");
CREATE INDEX "idx_audit_table" ON "audit_log" USING btree ("table_name", "row_id");
CREATE INDEX "idx_audit_payload" ON "audit_log" USING gin  ("payload");
