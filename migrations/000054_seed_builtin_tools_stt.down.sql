-- Remove STT seed row only if settings are empty (user may have configured it).
DELETE FROM builtin_tools WHERE name = 'stt' AND settings = '{}';
