-- Выполняется один раз при первом запуске контейнера Postgres.
-- У каждого микросервиса своя база данных (принцип database-per-service).
CREATE DATABASE auth_db;
CREATE DATABASE video_db;
CREATE DATABASE interaction_db;
