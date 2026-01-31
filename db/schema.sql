create type update_protocol as enum ('expo', 'codepush');

create table projects
(
    id              uuid                                  not null primary key,
    name            varchar(512)                          not null,
    update_protocol update_protocol                       not null,
    created_at      timestamptz default CURRENT_TIMESTAMP not null
);

create type update_status as enum (
    'empty',
    'pending',
    'processing',
    'published',
    'failed',
    'canceled'
);

create table updates
(
    id              uuid                                    not null primary key,
    project_id      uuid                                    not null,
    runtime_version varchar(64)                             not null,
    status          update_status default 'empty' :: update_status not null,
    message         varchar(512),
    channel         varchar(512)  default 'production'      not null,
    created_at      timestamptz   default CURRENT_TIMESTAMP not null,
    constraint fk_project_id foreign key (project_id) references projects (id)
);

create table update_assets
(
    id                  uuid                                  not null primary key,
    update_id           uuid                                  not null,
    storage_object_path varchar(512)                          not null,
    content_type        varchar(32)                           not null,
    extension           varchar(32)                            not null,
    content_md5         varchar(32)                           not null,
    content_sha256      varchar(64)                           not null,
    is_launch_asset     boolean                               not null,
    is_archive          boolean                               not null,
    platform            varchar(8)                            not null,
    content_length      bigint                                not null,
    created_at          timestamptz default CURRENT_TIMESTAMP not null,
    constraint fk_update_id foreign key (update_id) references updates (id)
);

create table update_metadata
(
    id              uuid                                  not null primary key,
    update_id       uuid                                  not null,
    expo_app_config jsonb                                 not null,
    created_at      timestamptz default CURRENT_TIMESTAMP not null,
    constraint fk_update_id foreign key (update_id) references updates (id)
);
