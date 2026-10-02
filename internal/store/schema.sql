-- 元数据库表结构。所有业务表挂在 t_scope 下，靠 scope_id 隔离。
-- 主数据与关系分离：实体进 t_dataset，连线进 t_mapping。

PRAGMA foreign_keys = ON;

-- 排班范围（如某学期晚自习）
CREATE TABLE IF NOT EXISTS t_scope (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT
);

-- 范围内的实体：subject / teacher / class / shift / binding / subject_tag
CREATE TABLE IF NOT EXISTS t_dataset (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    type       VARCHAR(64) NOT NULL,
    col1       VARCHAR(255),
    col2       VARCHAR(255),
    col3       VARCHAR(255),
    col4       VARCHAR(255),
    col5       VARCHAR(255),
    col6       VARCHAR(255),
    col7       VARCHAR(255),
    col8       VARCHAR(255),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    UNIQUE (id, scope_id),
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 规则正文：max_per_day / max_per_week / fair_count / fair_interval
CREATE TABLE IF NOT EXISTS t_rule (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    type       VARCHAR(64) NOT NULL,
    col1       VARCHAR(255),
    col2       VARCHAR(255),
    col3       VARCHAR(255),
    col4       VARCHAR(255),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    UNIQUE (id, scope_id),
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 日历：每个范围一条有效记录
CREATE TABLE IF NOT EXISTS t_calendar (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    start_date TEXT NOT NULL,
    end_date   TEXT NOT NULL,
    monday     INTEGER NOT NULL,
    tuesday    INTEGER NOT NULL,
    wednesday  INTEGER NOT NULL,
    thursday   INTEGER NOT NULL,
    friday     INTEGER NOT NULL,
    saturday   INTEGER NOT NULL,
    sunday     INTEGER NOT NULL,
    solver     VARCHAR(64) NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 排班日：由日历展开得到
CREATE TABLE IF NOT EXISTS t_schedule_day (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    day        TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 值班约束：某天 + 某班次 + 某老师，required = 1 必须值班，0 休息
CREATE TABLE IF NOT EXISTS t_duty (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    day        TEXT NOT NULL,
    shift_id   INTEGER NOT NULL,
    teacher_id INTEGER NOT NULL,
    required   INTEGER NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT,
    FOREIGN KEY (shift_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT,
    FOREIGN KEY (teacher_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT
);

-- 休息日
CREATE TABLE IF NOT EXISTS t_day_off (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    day        TEXT NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 课表快照版本
CREATE TABLE IF NOT EXISTS t_version (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    name       TEXT,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT
);

-- 课表格子：version_id 为空是当前课表，有值则为某版本快照
CREATE TABLE IF NOT EXISTS t_assignment (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    version_id INTEGER,
    day        TEXT NOT NULL,
    shift_id   INTEGER NOT NULL,
    class_id   INTEGER NOT NULL,
    teacher_id INTEGER,
    locked     INTEGER NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT,
    FOREIGN KEY (version_id) REFERENCES t_version(id) ON DELETE RESTRICT,
    FOREIGN KEY (shift_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT,
    FOREIGN KEY (class_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT,
    FOREIGN KEY (teacher_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT
);

-- 范围内的连线：from_id / to_id 指向 t_dataset，rule_id 指向 t_rule
-- subject_tag 类型即「学科挂标签」：from_id 是学科，to_id 是标签
CREATE TABLE IF NOT EXISTS t_mapping (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_id   INTEGER NOT NULL,
    type       VARCHAR(64) NOT NULL,
    from_id    INTEGER,
    to_id      INTEGER,
    col1       VARCHAR(255),
    col2       VARCHAR(255),
    col3       VARCHAR(255),
    col4       VARCHAR(255),
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL,
    deleted_at BIGINT,
    rule_id    INTEGER,
    FOREIGN KEY (scope_id) REFERENCES t_scope(id) ON DELETE RESTRICT,
    FOREIGN KEY (from_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT,
    FOREIGN KEY (to_id, scope_id) REFERENCES t_dataset(id, scope_id) ON DELETE RESTRICT,
    FOREIGN KEY (rule_id, scope_id) REFERENCES t_rule(id, scope_id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_scope_name_active
    ON t_scope(name) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_mapping_active
    ON t_mapping(scope_id, type, from_id, to_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_calendar_scope_active
    ON t_calendar(scope_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_day_off_active
    ON t_day_off(scope_id, day) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_day_active
    ON t_schedule_day(scope_id, day) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_duty_active
    ON t_duty(scope_id, day, shift_id, teacher_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_mapping_global_rule
    ON t_mapping(scope_id, rule_id) WHERE deleted_at IS NULL AND type = 'global_rule';
CREATE UNIQUE INDEX IF NOT EXISTS idx_mapping_personal_rule
    ON t_mapping(scope_id, from_id, rule_id) WHERE deleted_at IS NULL AND type = 'personal_rule';
CREATE UNIQUE INDEX IF NOT EXISTS idx_assignment_current
    ON t_assignment(scope_id, day, shift_id, class_id) WHERE deleted_at IS NULL AND version_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_assignment_version
    ON t_assignment(scope_id, version_id, day, shift_id, class_id) WHERE deleted_at IS NULL AND version_id IS NOT NULL;
