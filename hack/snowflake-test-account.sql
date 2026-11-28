-- Sets up Changeport's Snowflake test account for the destination's tests
-- (README.md, Snowflake): a monthly cap on credits, a warehouse that
-- suspends after a minute, and a role and key-pair user for the tests.
-- Run it in a Snowsight worksheet as ACCOUNTADMIN, with the test user's
-- public key in place of the placeholder below; running it again changes
-- nothing but the key, which is how the key is rotated.

USE ROLE ACCOUNTADMIN;

-- The account stops at 20 credits a month: a warning email to the account
-- admins at 75%, its warehouses suspended once running queries finish at
-- 100%, and at once at 110%.
CREATE RESOURCE MONITOR IF NOT EXISTS CHANGEPORT_MONTHLY
  WITH CREDIT_QUOTA = 20 FREQUENCY = MONTHLY START_TIMESTAMP = IMMEDIATELY;
ALTER RESOURCE MONITOR CHANGEPORT_MONTHLY SET CREDIT_QUOTA = 20
  TRIGGERS ON 75 PERCENT DO NOTIFY
           ON 100 PERCENT DO SUSPEND
           ON 110 PERCENT DO SUSPEND_IMMEDIATE;
ALTER ACCOUNT SET RESOURCE_MONITOR = CHANGEPORT_MONTHLY;

CREATE WAREHOUSE IF NOT EXISTS CHANGEPORT_TEST_WH
  WAREHOUSE_SIZE = XSMALL AUTO_SUSPEND = 60 AUTO_RESUME = TRUE INITIALLY_SUSPENDED = TRUE;

-- The tests' role owns their database, where each run makes a schema of its
-- own, as Snowflake cannot limit a role to databases of some names. It may
-- also create users, roles and network policies, to play a customer running
-- the setup SQL the console generates; nothing else lives in this account.
CREATE ROLE IF NOT EXISTS CHANGEPORT_TEST;
GRANT ROLE CHANGEPORT_TEST TO ROLE SYSADMIN;
CREATE DATABASE IF NOT EXISTS CHANGEPORT_TEST;
GRANT OWNERSHIP ON DATABASE CHANGEPORT_TEST TO ROLE CHANGEPORT_TEST COPY CURRENT GRANTS;
GRANT OWNERSHIP ON SCHEMA CHANGEPORT_TEST.PUBLIC TO ROLE CHANGEPORT_TEST COPY CURRENT GRANTS;
GRANT USAGE ON WAREHOUSE CHANGEPORT_TEST_WH TO ROLE CHANGEPORT_TEST;
GRANT CREATE USER, CREATE ROLE, CREATE NETWORK POLICY ON ACCOUNT TO ROLE CHANGEPORT_TEST;

-- The tests sign in with a key pair only (SNOWFLAKE_TEST_PRIVATE_KEY in
-- .env and CI's secrets); a service user has no password.
CREATE USER IF NOT EXISTS CHANGEPORT_TEST_CI
  TYPE = SERVICE
  DEFAULT_ROLE = CHANGEPORT_TEST
  DEFAULT_WAREHOUSE = CHANGEPORT_TEST_WH
  DEFAULT_NAMESPACE = CHANGEPORT_TEST.PUBLIC;
ALTER USER CHANGEPORT_TEST_CI SET RSA_PUBLIC_KEY = '<the public key, without its BEGIN and END lines>';
GRANT ROLE CHANGEPORT_TEST TO USER CHANGEPORT_TEST_CI;
