-- One row gates Registry garbage collection against build claims. A live lease
-- (lease_until in the future) or a set draining_since refuses every claim; the
-- GC worker takes the lease only while no build runs. last_completed_at starts
-- at installation, so a fresh platform drains no earlier than one day later.
CREATE TABLE registry_gc_state (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    lease_until TIMESTAMPTZ,
    draining_since TIMESTAMPTZ,
    last_completed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO registry_gc_state (id) VALUES (TRUE);
