-- migrate:up
CREATE INDEX projects_name_idx ON projects (name);

-- migrate:down
DROP INDEX projects_name_idx;
