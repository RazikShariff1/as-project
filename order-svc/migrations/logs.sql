-- order_logs in the reference's shape: an action and its details (JSON), instead of kind/from/to/note columns.
ALTER TABLE order_logs RENAME COLUMN kind TO action;
ALTER TABLE order_logs ADD COLUMN details JSONB NOT NULL DEFAULT '{}';

UPDATE order_logs SET details = jsonb_strip_nulls(jsonb_build_object('from', from_value, 'to', to_value, 'note', NULLIF(note, '')));

ALTER TABLE order_logs DROP COLUMN from_value;
ALTER TABLE order_logs DROP COLUMN to_value;
ALTER TABLE order_logs DROP COLUMN note;
