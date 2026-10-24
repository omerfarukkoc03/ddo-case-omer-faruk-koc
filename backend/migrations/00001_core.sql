-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- ---------------------------------------------------------------------------
-- Sahipler, kullanıcılar, kimlik
-- ---------------------------------------------------------------------------
CREATE TABLE owners (
    id           bigserial PRIMARY KEY,
    full_name    text        NOT NULL CHECK (length(trim(full_name)) >= 2),
    phone        text        NOT NULL CHECK (phone ~ '^\+?[0-9]{10,15}$'),
    email        text        NULL,
    address      text        NULL,
    merged_into  bigint      NULL REFERENCES owners(id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (merged_into IS NULL OR merged_into <> id)
);
CREATE INDEX owners_phone_idx       ON owners (phone) WHERE merged_into IS NULL;
CREATE INDEX owners_name_idx        ON owners (lower(full_name) text_pattern_ops) WHERE merged_into IS NULL;
CREATE INDEX owners_keyset_idx      ON owners (id) WHERE merged_into IS NULL;

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    full_name     text        NOT NULL,
    role          text        NOT NULL CHECK (role IN ('owner','vet','technician','reception','pharmacy')),
    owner_id      bigint      NULL REFERENCES owners(id),
    is_active     boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    -- "owner" rolündeki kullanıcı mutlaka bir sahip kaydına bağlıdır, diğerleri bağlanamaz.
    CHECK ((role = 'owner') = (owner_id IS NOT NULL))
);
CREATE UNIQUE INDEX users_email_uq ON users (lower(email));

CREATE TABLE refresh_tokens (
    id          bigserial PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users(id),
    token_hash  bytea       NOT NULL UNIQUE,
    family_id   uuid        NOT NULL,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);

-- ---------------------------------------------------------------------------
-- Hayvanlar ve çoklu sahiplik
-- ---------------------------------------------------------------------------
CREATE TABLE animals (
    id                   bigserial PRIMARY KEY,
    name                 text          NOT NULL CHECK (length(trim(name)) >= 1),
    species              text          NOT NULL CHECK (species IN ('dog','cat','rabbit')),
    breed                text          NOT NULL DEFAULT 'mixed',
    sex                  text          NOT NULL DEFAULT 'unknown' CHECK (sex IN ('male','female','unknown')),
    microchip            text          NULL CHECK (microchip ~ '^[0-9]{15}$'),
    -- Doğum tarihi bilinmiyorsa kayıttaki tahmini yaştan hesaplanır ve bayrakla işaretlenir.
    birth_date           date          NOT NULL,
    birth_date_estimated boolean       NOT NULL DEFAULT false,
    weight_kg            numeric(7,3)  NULL CHECK (weight_kg > 0),
    merged_into          bigint        NULL REFERENCES animals(id),
    created_at           timestamptz   NOT NULL DEFAULT now(),
    updated_at           timestamptz   NOT NULL DEFAULT now(),
    CHECK (merged_into IS NULL OR merged_into <> id)
);
-- Aktif kayıtlar arasında mikroçip tekildir; birleştirilmiş (pasif) kayıtlar kısıtın dışındadır.
CREATE UNIQUE INDEX animals_microchip_uq ON animals (microchip) WHERE microchip IS NOT NULL AND merged_into IS NULL;
CREATE INDEX animals_keyset_idx ON animals (id) WHERE merged_into IS NULL;
CREATE INDEX animals_name_idx   ON animals (lower(name) text_pattern_ops) WHERE merged_into IS NULL;

CREATE TABLE animal_owners (
    animal_id   bigint      NOT NULL REFERENCES animals(id),
    owner_id    bigint      NOT NULL REFERENCES owners(id),
    relation    text        NOT NULL DEFAULT 'owner' CHECK (relation IN ('owner','co_owner','caretaker')),
    is_billing  boolean     NOT NULL DEFAULT false,
    since       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (animal_id, owner_id)
);
-- Bir hayvanın en fazla bir fatura sorumlusu olabilir.
CREATE UNIQUE INDEX animal_owners_billing_uq ON animal_owners (animal_id) WHERE is_billing;
CREATE INDEX animal_owners_owner_idx ON animal_owners (owner_id);

CREATE TABLE animal_weights (
    id          bigserial PRIMARY KEY,
    animal_id   bigint       NOT NULL REFERENCES animals(id),
    weight_kg   numeric(7,3) NOT NULL CHECK (weight_kg > 0),
    measured_at timestamptz  NOT NULL DEFAULT now(),
    recorded_by bigint       NULL REFERENCES users(id)
);
CREATE INDEX animal_weights_animal_idx ON animal_weights (animal_id, measured_at);

-- ---------------------------------------------------------------------------
-- Aşılar (T-1)
-- ---------------------------------------------------------------------------
CREATE TABLE vaccines (
    id      bigserial PRIMARY KEY,
    code    text NOT NULL UNIQUE,
    name    text NOT NULL,
    species text NOT NULL CHECK (species IN ('dog','cat','rabbit'))
);

CREATE TABLE vaccine_protocols (
    id                    bigserial PRIMARY KEY,
    vaccine_id            bigint  NOT NULL REFERENCES vaccines(id),
    species               text    NOT NULL CHECK (species IN ('dog','cat','rabbit')),
    breed                 text    NULL, -- NULL: türün tüm ırkları; dolu: ırka özel protokol önceliklidir
    series_weeks          int[]   NOT NULL CHECK (cardinality(series_weeks) >= 1),
    min_interval_days     int     NOT NULL CHECK (min_interval_days > 0),
    late_start_weeks      int     NOT NULL CHECK (late_start_weeks > 0),
    late_start_doses      int     NOT NULL CHECK (late_start_doses >= 1),
    late_interval_days    int     NOT NULL CHECK (late_interval_days > 0),
    booster_interval_days int     NOT NULL CHECK (booster_interval_days > 0),
    UNIQUE NULLS NOT DISTINCT (vaccine_id, species, breed)
);

CREATE TABLE vaccine_doses (
    id               bigserial PRIMARY KEY,
    animal_id        bigint      NOT NULL REFERENCES animals(id),
    vaccine_id       bigint      NOT NULL REFERENCES vaccines(id),
    dose_number      int         NOT NULL CHECK (dose_number >= 1),
    kind             text        NOT NULL CHECK (kind IN ('primary','booster')),
    due_date         date        NOT NULL,
    status           text        NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','administered','cancelled')),
    administered_at  timestamptz NULL,
    administered_by  bigint      NULL REFERENCES users(id),
    batch_no         text        NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'administered') = (administered_at IS NOT NULL))
);
CREATE INDEX vaccine_doses_due_idx ON vaccine_doses (due_date) WHERE status = 'planned';
CREATE INDEX vaccine_doses_animal_idx ON vaccine_doses (animal_id, vaccine_id);

CREATE TABLE reminders (
    id            bigserial PRIMARY KEY,
    dose_id       bigint      NOT NULL REFERENCES vaccine_doses(id),
    owner_id      bigint      NOT NULL REFERENCES owners(id),
    kind          text        NOT NULL CHECK (kind IN ('upcoming','due','overdue')),
    due_date      date        NOT NULL,
    channel       text        NOT NULL DEFAULT 'email',
    created_at    timestamptz NOT NULL DEFAULT now(),
    sent_at       timestamptz NULL,
    -- İdempotans: aynı doz + tür + vade için ikinci hatırlatma oluşamaz.
    UNIQUE (dose_id, kind, due_date)
);
CREATE INDEX reminders_owner_idx ON reminders (owner_id, created_at DESC);

CREATE TABLE job_runs (
    id          bigserial PRIMARY KEY,
    job         text        NOT NULL,
    run_date    date        NOT NULL,
    started_at  timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz NULL,
    created     int         NOT NULL DEFAULT 0
);

-- ---------------------------------------------------------------------------
-- Kaynaklar, randevular, muayene (T-3)
-- ---------------------------------------------------------------------------
CREATE TABLE resources (
    id        bigserial PRIMARY KEY,
    kind      text    NOT NULL CHECK (kind IN ('vet','exam_room','surgery_room')),
    name      text    NOT NULL UNIQUE,
    user_id   bigint  NULL REFERENCES users(id),
    is_active boolean NOT NULL DEFAULT true,
    CHECK ((kind = 'vet') = (user_id IS NOT NULL))
);

CREATE TABLE appointments (
    id                bigserial PRIMARY KEY,
    animal_id         bigint      NOT NULL REFERENCES animals(id),
    vet_id            bigint      NOT NULL REFERENCES resources(id),
    room_id           bigint      NOT NULL REFERENCES resources(id),
    kind              text        NOT NULL CHECK (kind IN ('exam','vaccination','surgery','control','emergency')),
    priority          text        NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal','emergency')),
    starts_at         timestamptz NOT NULL,
    ends_at           timestamptz NOT NULL,
    status            text        NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','checked_in','completed','cancelled','no_show')),
    reason            text        NULL,
    original_starts_at timestamptz NULL,
    reschedule_count  int         NOT NULL DEFAULT 0,
    created_by        bigint      NULL REFERENCES users(id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    CHECK (ends_at > starts_at),
    CHECK (vet_id <> room_id),
    -- Aynı veteriner ya da aynı oda aynı anda iki aktif randevuda olamaz (veritabanı garantisi).
    CONSTRAINT appointments_vet_no_overlap EXCLUDE USING gist (
        vet_id WITH =, tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('scheduled','checked_in')) DEFERRABLE INITIALLY IMMEDIATE,
    CONSTRAINT appointments_room_no_overlap EXCLUDE USING gist (
        room_id WITH =, tstzrange(starts_at, ends_at, '[)') WITH &&
    ) WHERE (status IN ('scheduled','checked_in')) DEFERRABLE INITIALLY IMMEDIATE
);
CREATE INDEX appointments_animal_idx ON appointments (animal_id, starts_at DESC);
CREATE INDEX appointments_day_idx    ON appointments (starts_at, id);

CREATE TABLE examinations (
    id             bigserial PRIMARY KEY,
    appointment_id bigint       NULL UNIQUE REFERENCES appointments(id),
    animal_id      bigint       NOT NULL REFERENCES animals(id),
    vet_user_id    bigint       NOT NULL REFERENCES users(id),
    weight_kg      numeric(7,3) NULL CHECK (weight_kg > 0),
    temperature_c  numeric(4,1) NULL CHECK (temperature_c BETWEEN 30 AND 45),
    findings       text         NOT NULL,
    diagnosis      text         NULL,
    created_at     timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX examinations_animal_idx ON examinations (animal_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- İlaç, reçete, stok (T-2)
-- ---------------------------------------------------------------------------
CREATE TABLE drugs (
    id                      bigserial PRIMARY KEY,
    name                    text          NOT NULL UNIQUE,
    active_ingredient       text          NOT NULL,
    form                    text          NOT NULL CHECK (form IN ('injection','oral_liquid','tablet')),
    concentration_mg_per_ml numeric(10,3) NULL CHECK (concentration_mg_per_ml > 0),
    tablet_mg               numeric(10,3) NULL CHECK (tablet_mg > 0),
    stock_unit              text          NOT NULL CHECK (stock_unit IN ('ml','tablet')),
    unit_price_kurus        bigint        NOT NULL CHECK (unit_price_kurus >= 0),
    CHECK ((form = 'tablet') = (tablet_mg IS NOT NULL)),
    CHECK ((form <> 'tablet') = (concentration_mg_per_ml IS NOT NULL))
);

CREATE TABLE drug_dosing (
    drug_id            bigint        NOT NULL REFERENCES drugs(id),
    species            text          NOT NULL CHECK (species IN ('dog','cat','rabbit')),
    mg_per_kg_min      numeric(10,4) NOT NULL CHECK (mg_per_kg_min > 0),
    mg_per_kg_max      numeric(10,4) NOT NULL,
    max_single_dose_mg numeric(10,3) NULL CHECK (max_single_dose_mg > 0),
    PRIMARY KEY (drug_id, species),
    CHECK (mg_per_kg_max >= mg_per_kg_min)
);

CREATE TABLE drug_contraindications (
    id      bigserial PRIMARY KEY,
    drug_id bigint NOT NULL REFERENCES drugs(id),
    species text   NOT NULL CHECK (species IN ('dog','cat','rabbit')),
    breed   text   NULL, -- NULL: türün tamamı
    reason  text   NOT NULL,
    UNIQUE NULLS NOT DISTINCT (drug_id, species, breed)
);

CREATE TABLE prescriptions (
    id                  bigserial PRIMARY KEY,
    animal_id           bigint        NOT NULL REFERENCES animals(id),
    examination_id      bigint        NULL REFERENCES examinations(id),
    drug_id             bigint        NOT NULL REFERENCES drugs(id),
    vet_user_id         bigint        NOT NULL REFERENCES users(id),
    -- Reçete yazıldığı andaki girdilerin ve hesabın kopyası (sonradan kilo değişse de değişmez).
    species_snapshot    text          NOT NULL,
    weight_kg_snapshot  numeric(7,3)  NOT NULL CHECK (weight_kg_snapshot > 0),
    mg_per_kg           numeric(10,4) NOT NULL CHECK (mg_per_kg > 0),
    dose_mg             numeric(12,3) NOT NULL CHECK (dose_mg > 0),
    dose_volume_ml      numeric(12,3) NULL,
    dose_tablets        numeric(8,2)  NULL,
    doses_per_day       int           NOT NULL CHECK (doses_per_day BETWEEN 1 AND 6),
    duration_days       int           NOT NULL CHECK (duration_days BETWEEN 1 AND 90),
    dispense_quantity   int           NOT NULL CHECK (dispense_quantity > 0),
    calculation         jsonb         NOT NULL,
    exceeds_max         boolean       NOT NULL DEFAULT false,
    override_reason     text          NULL,
    status              text          NOT NULL DEFAULT 'active' CHECK (status IN ('active','dispensed','cancelled')),
    created_at          timestamptz   NOT NULL DEFAULT now(),
    -- Sınırı aşan doz ancak gerekçeyle yazılabilir.
    CHECK (NOT exceeds_max OR length(trim(coalesce(override_reason,''))) >= 10)
);
CREATE INDEX prescriptions_animal_idx ON prescriptions (animal_id, created_at DESC);
CREATE INDEX prescriptions_status_idx ON prescriptions (status, id) WHERE status = 'active';

CREATE TABLE stock_batches (
    id          bigserial PRIMARY KEY,
    drug_id     bigint      NOT NULL REFERENCES drugs(id),
    lot_no      text        NOT NULL,
    expires_on  date        NOT NULL,
    quantity    int         NOT NULL CHECK (quantity >= 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (drug_id, lot_no)
);
CREATE INDEX stock_batches_fefo_idx ON stock_batches (drug_id, expires_on) WHERE quantity > 0;

CREATE TABLE stock_movements (
    id              bigserial PRIMARY KEY,
    batch_id        bigint      NOT NULL REFERENCES stock_batches(id),
    prescription_id bigint      NULL REFERENCES prescriptions(id),
    delta           int         NOT NULL CHECK (delta <> 0),
    reason          text        NOT NULL CHECK (reason IN ('receive','dispense','adjust','expire')),
    created_by      bigint      NULL REFERENCES users(id),
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX stock_movements_batch_idx ON stock_movements (batch_id, created_at);

-- ---------------------------------------------------------------------------
-- Yatan hasta ve kafes (F-5, T-3)
-- ---------------------------------------------------------------------------
CREATE TABLE cages (
    id         bigserial PRIMARY KEY,
    code       text        NOT NULL UNIQUE,
    size       text        NOT NULL CHECK (size IN ('S','M','L')),
    ward       text        NOT NULL DEFAULT 'general' CHECK (ward IN ('general','isolation')),
    state      text        NOT NULL DEFAULT 'empty' CHECK (state IN ('empty','occupied','cleaning','ready')),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE cage_events (
    id         bigserial PRIMARY KEY,
    cage_id    bigint      NOT NULL REFERENCES cages(id),
    from_state text        NOT NULL,
    to_state   text        NOT NULL,
    actor_id   bigint      NULL REFERENCES users(id),
    at         timestamptz NOT NULL DEFAULT now()
);

-- Kafes durum makinesi: boş → dolu → temizlik → hazır → dolu. Diğer her geçiş reddedilir.
CREATE FUNCTION cage_state_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.state = OLD.state THEN
        RETURN NEW;
    END IF;
    IF NOT ((OLD.state = 'empty'    AND NEW.state = 'occupied')
         OR (OLD.state = 'occupied' AND NEW.state = 'cleaning')
         OR (OLD.state = 'cleaning' AND NEW.state = 'ready')
         OR (OLD.state = 'ready'    AND NEW.state = 'occupied')) THEN
        RAISE EXCEPTION 'invalid cage transition % -> %', OLD.state, NEW.state
            USING ERRCODE = 'P0001', HINT = 'cage_invalid_transition';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END $$;
CREATE TRIGGER cages_state_guard BEFORE UPDATE OF state ON cages
    FOR EACH ROW EXECUTE FUNCTION cage_state_guard();

CREATE TABLE hospitalizations (
    id            bigserial PRIMARY KEY,
    animal_id     bigint      NOT NULL REFERENCES animals(id),
    cage_id       bigint      NOT NULL REFERENCES cages(id),
    vet_user_id   bigint      NOT NULL REFERENCES users(id),
    reason        text        NOT NULL,
    status        text        NOT NULL DEFAULT 'active' CHECK (status IN ('active','discharged')),
    admitted_at   timestamptz NOT NULL DEFAULT now(),
    discharged_at timestamptz NULL,
    CHECK ((status = 'discharged') = (discharged_at IS NOT NULL))
);
CREATE UNIQUE INDEX hospitalizations_animal_active_uq ON hospitalizations (animal_id) WHERE status = 'active';
CREATE UNIQUE INDEX hospitalizations_cage_active_uq   ON hospitalizations (cage_id)   WHERE status = 'active';

CREATE TABLE hospital_notes (
    id                 bigserial PRIMARY KEY,
    hospitalization_id bigint      NOT NULL REFERENCES hospitalizations(id),
    author_id          bigint      NOT NULL REFERENCES users(id),
    note               text        NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Fatura, tahsilat, paketler (F-6, T-4)
-- ---------------------------------------------------------------------------
CREATE TABLE invoices (
    id           bigserial PRIMARY KEY,
    animal_id    bigint      NOT NULL REFERENCES animals(id),
    owner_id     bigint      NOT NULL REFERENCES owners(id),
    billing_rule text        NOT NULL CHECK (billing_rule IN ('billing_owner','first_owner','explicit')),
    status       text        NOT NULL DEFAULT 'open' CHECK (status IN ('open','paid','void')),
    total_kurus  bigint      NOT NULL DEFAULT 0 CHECK (total_kurus >= 0),
    paid_kurus   bigint      NOT NULL DEFAULT 0 CHECK (paid_kurus >= 0),
    created_by   bigint      NULL REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    -- Fazla tahsilat imkânsız.
    CHECK (paid_kurus <= total_kurus)
);
CREATE INDEX invoices_owner_idx  ON invoices (owner_id, id DESC);
CREATE INDEX invoices_animal_idx ON invoices (animal_id);
CREATE INDEX invoices_open_idx   ON invoices (id) WHERE status = 'open';

CREATE TABLE invoice_lines (
    id               bigserial PRIMARY KEY,
    invoice_id       bigint NOT NULL REFERENCES invoices(id),
    description      text   NOT NULL,
    quantity         int    NOT NULL CHECK (quantity > 0),
    unit_price_kurus bigint NOT NULL CHECK (unit_price_kurus >= 0),
    line_total_kurus bigint NOT NULL CHECK (line_total_kurus = quantity * unit_price_kurus)
);
CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id);

CREATE TABLE payments (
    id           bigserial PRIMARY KEY,
    invoice_id   bigint      NOT NULL REFERENCES invoices(id),
    amount_kurus bigint      NOT NULL CHECK (amount_kurus > 0),
    method       text        NOT NULL CHECK (method IN ('cash','card','transfer')),
    received_by  bigint      NULL REFERENCES users(id),
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX payments_invoice_idx ON payments (invoice_id);

CREATE TABLE packages (
    id           bigserial PRIMARY KEY,
    name         text   NOT NULL UNIQUE,
    species      text   NOT NULL CHECK (species IN ('dog','cat','rabbit')),
    sessions     int    NOT NULL CHECK (sessions > 0),
    price_kurus  bigint NOT NULL CHECK (price_kurus >= 0)
);

CREATE TABLE package_purchases (
    id             bigserial PRIMARY KEY,
    package_id     bigint      NOT NULL REFERENCES packages(id),
    animal_id      bigint      NOT NULL REFERENCES animals(id),
    owner_id       bigint      NOT NULL REFERENCES owners(id),
    sessions_total int         NOT NULL CHECK (sessions_total > 0),
    sessions_left  int         NOT NULL,
    purchased_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (sessions_left BETWEEN 0 AND sessions_total)
);
CREATE INDEX package_purchases_animal_idx ON package_purchases (animal_id);

CREATE TABLE package_redemptions (
    id          bigserial PRIMARY KEY,
    purchase_id bigint      NOT NULL REFERENCES package_purchases(id),
    dose_id     bigint      NOT NULL UNIQUE REFERENCES vaccine_doses(id), -- bir doz en fazla bir seans düşer
    redeemed_by bigint      NULL REFERENCES users(id),
    redeemed_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX package_redemptions_purchase_idx ON package_redemptions (purchase_id);

-- ---------------------------------------------------------------------------
-- Mükerrer kayıt birleştirme (T-4)
-- ---------------------------------------------------------------------------
CREATE TABLE merge_operations (
    id            bigserial PRIMARY KEY,
    entity        text        NOT NULL CHECK (entity IN ('owner','animal')),
    survivor_id   bigint      NOT NULL,
    duplicate_id  bigint      NOT NULL,
    snapshot      jsonb       NOT NULL,
    performed_by  bigint      NULL REFERENCES users(id),
    performed_at  timestamptz NOT NULL DEFAULT now(),
    undone_at     timestamptz NULL,
    undone_by     bigint      NULL REFERENCES users(id),
    CHECK (survivor_id <> duplicate_id)
);
CREATE UNIQUE INDEX merge_operations_active_uq ON merge_operations (entity, duplicate_id) WHERE undone_at IS NULL;

-- ---------------------------------------------------------------------------
-- Denetim kaydı (append-only) ve idempotans
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    at          timestamptz NOT NULL DEFAULT now(),
    actor_id    bigint      NULL,
    actor_role  text        NULL,
    action      text        NOT NULL,
    entity      text        NOT NULL,
    entity_id   text        NOT NULL,
    old_value   jsonb       NULL,
    new_value   jsonb       NULL,
    request_id  text        NULL
);
CREATE INDEX audit_log_entity_idx ON audit_log (entity, entity_id, id DESC);

CREATE FUNCTION audit_log_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only' USING ERRCODE = 'P0001', HINT = 'audit_append_only';
END $$;
CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();
CREATE TRIGGER audit_log_no_truncate BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION audit_log_immutable();

-- Reçetenin hesap alanları yazıldıktan sonra değiştirilemez; yalnız durum alanı ilerleyebilir.
CREATE FUNCTION prescription_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.weight_kg_snapshot, NEW.mg_per_kg, NEW.dose_mg, NEW.dose_volume_ml, NEW.dose_tablets,
        NEW.calculation, NEW.drug_id, NEW.species_snapshot, NEW.exceeds_max, NEW.override_reason)
       IS DISTINCT FROM
       (OLD.weight_kg_snapshot, OLD.mg_per_kg, OLD.dose_mg, OLD.dose_volume_ml, OLD.dose_tablets,
        OLD.calculation, OLD.drug_id, OLD.species_snapshot, OLD.exceeds_max, OLD.override_reason) THEN
        RAISE EXCEPTION 'prescription calculation is immutable' USING ERRCODE = 'P0001', HINT = 'prescription_immutable';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER prescriptions_immutable BEFORE UPDATE ON prescriptions
    FOR EACH ROW EXECUTE FUNCTION prescription_immutable();

CREATE TABLE idempotency_keys (
    user_id       bigint      NOT NULL,
    key           text        NOT NULL CHECK (length(key) BETWEEN 8 AND 128),
    method        text        NOT NULL,
    path          text        NOT NULL,
    request_hash  bytea       NOT NULL,
    status_code   int         NULL, -- NULL: istek hâlâ işleniyor
    response_body bytea       NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
-- +goose StatementEnd
