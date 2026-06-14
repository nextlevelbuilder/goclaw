-- Reverse 000068: drop Meow poster tables (children first for FK order).
DROP TABLE IF EXISTS mp_reports_sent;
DROP TABLE IF EXISTS mp_smm_orders;
DROP TABLE IF EXISTS mp_channel_metrics;
DROP TABLE IF EXISTS mp_content_posts;
DROP TABLE IF EXISTS mp_channels;
