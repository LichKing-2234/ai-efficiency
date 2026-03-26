-- Create databases for local development
CREATE DATABASE ai_efficiency;
CREATE DATABASE sub2api;

-- Create read-only user for sub2api access
CREATE USER ae_readonly WITH PASSWORD 'ae_readonly';
GRANT CONNECT ON DATABASE sub2api TO ae_readonly;

\c sub2api
GRANT USAGE ON SCHEMA public TO ae_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO ae_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO ae_readonly;
