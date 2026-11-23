-- migrate:up
CREATE TABLE tasks (
    id         INTEGER PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects (id),
    title      TEXT NOT NULL
);

-- migrate:down
DROP TABLE tasks;
