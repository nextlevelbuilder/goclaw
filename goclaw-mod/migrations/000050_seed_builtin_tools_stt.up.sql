-- Seed the STT builtin_tools row. ON CONFLICT preserves user-customized settings.
INSERT INTO builtin_tools (name, display_name, description, category, enabled, settings)
VALUES ('stt', 'Speech-to-Text', 'Transcribe voice/audio messages to text using ElevenLabs Scribe or a proxy service', 'media', true, '{}')
ON CONFLICT (name) DO NOTHING;
