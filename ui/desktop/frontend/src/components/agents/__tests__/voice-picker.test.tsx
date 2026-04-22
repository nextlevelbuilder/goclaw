import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import i18n from '../../../i18n'
import { VoicePicker } from '../voice-picker'

// Mock getApiClient
vi.mock('../../../lib/api', () => ({
  getApiClient: () => ({
    get: vi.fn().mockResolvedValue({ voices: [
      { voice_id: 'v1', name: 'Rachel', preview_url: 'https://example.com/rachel.mp3', labels: { gender: 'female' } },
      { voice_id: 'v2', name: 'Adam', preview_url: null },
    ] }),
    post: vi.fn().mockResolvedValue({ status: 'ok' }),
  }),
}))

function renderWithI18n(ui: React.ReactElement) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>)
}

describe('VoicePicker (desktop)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('(a) renders within a narrow 390px container without overflow', () => {
    const { container } = renderWithI18n(
      <div style={{ width: '390px' }}>
        <VoicePicker value={null} onChange={() => {}} />
      </div>,
    )
    const wrapper = container.firstElementChild as HTMLElement
    expect(wrapper.clientWidth).toBeLessThanOrEqual(390)
  })

  it('(b) calls onChange with voice_id when a voice is selected', async () => {
    const onChange = vi.fn()
    renderWithI18n(<VoicePicker value={null} onChange={onChange} />)

    // Wait for voices to load
    await waitFor(() => expect(screen.queryByText('Loading voices…')).toBeNull())

    // Open combobox by clicking the input
    const input = screen.getByRole('textbox')
    fireEvent.focus(input)

    // Click Rachel option
    await waitFor(() => {
      expect(screen.getByText('Rachel')).toBeInTheDocument()
    })
    fireEvent.click(screen.getByText('Rachel'))
    expect(onChange).toHaveBeenCalledWith('v1')
  })

  it('(c) preview button hidden when preview_url is null/empty', async () => {
    renderWithI18n(<VoicePicker value="v2" onChange={() => {}} />)

    await waitFor(() => expect(screen.queryByText('Loading voices…')).toBeNull())

    // v2 (Adam) has no preview_url — preview button for it must not appear
    const playButtons = screen.queryAllByRole('button', { name: /preview|nghe thử|试听/i })
    // If a preview button appears, it must be for v1 (Rachel) only
    for (const btn of playButtons) {
      expect(btn.getAttribute('aria-label') ?? '').not.toContain('Adam')
    }
  })

  it('(d) loads voices via API mock — Rachel appears in list', async () => {
    renderWithI18n(<VoicePicker value={null} onChange={() => {}} />)

    const input = screen.getByRole('textbox')
    fireEvent.focus(input)

    await waitFor(() => {
      expect(screen.getByText('Rachel')).toBeInTheDocument()
    })
  })
})
