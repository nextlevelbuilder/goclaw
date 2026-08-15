import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import i18n from '../../../i18n'
import { SttProviderForm } from '../stt-provider-form'

// Mock getApiClient (not called directly by form, but hooks import it)
vi.mock('../../../lib/api', () => ({
  getApiClient: () => ({
    get: vi.fn().mockResolvedValue({ voices: [] }),
    post: vi.fn().mockResolvedValue({ status: 'ok' }),
    put: vi.fn().mockResolvedValue({}),
  }),
}))

function renderForm(
  onSave = vi.fn().mockResolvedValue(undefined),
  onCancel = vi.fn(),
  initialSettings: Record<string, unknown> = {},
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <SttProviderForm
        initialSettings={initialSettings}
        onSave={onSave}
        onCancel={onCancel}
      />
    </I18nextProvider>,
  )
}

describe('SttProviderForm (desktop)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('(a) renders all fields including whatsapp_enabled toggle', () => {
    renderForm()

    // Provider checkboxes
    expect(screen.getByRole('checkbox', { name: /elevenlabs/i })).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /proxy/i })).toBeInTheDocument()

    // ElevenLabs fields
    expect(screen.getByPlaceholderText('xi-...')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('en')).toBeInTheDocument()

    // Proxy fields
    expect(screen.getByPlaceholderText('https://...')).toBeInTheDocument()

    // WhatsApp toggle
    expect(screen.getByRole('checkbox', { name: /whatsapp/i })).toBeInTheDocument()
  })

  it('(b) privacy banner renders above the whatsapp toggle', () => {
    renderForm()

    const banner = screen.getByTestId('whatsapp-privacy-banner')
    const toggle = screen.getByRole('checkbox', { name: /whatsapp/i })

    expect(banner).toBeInTheDocument()
    // Banner should appear before toggle in DOM order
    expect(
      banner.compareDocumentPosition(toggle) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('(c) submits via onSave with correct payload shape', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    renderForm(onSave, vi.fn(), {
      providers: ['elevenlabs'],
      elevenlabs: { api_key: 'xi-test', default_language: 'vi' },
      proxy: { url: '', api_key: '', tenant_id: '' },
      whatsapp_enabled: false,
    })

    // Toggle whatsapp on
    const whatsappToggle = screen.getByRole('checkbox', { name: /whatsapp/i })
    fireEvent.click(whatsappToggle)

    // Click Save
    fireEvent.click(screen.getByRole('button', { name: /^save$|^lưu$|^保存$/i }))

    await waitFor(() => {
      expect(onSave).toHaveBeenCalledTimes(1)
    })

    const payload = onSave.mock.calls[0][0] as Record<string, unknown>
    expect(payload.whatsapp_enabled).toBe(true)
    expect((payload.elevenlabs as Record<string, string>).api_key).toBe('xi-test')
    expect(Array.isArray(payload.providers)).toBe(true)
  })
})
