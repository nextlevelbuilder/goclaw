import { useState } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { Checkbox } from '../Checkbox'
import { Field } from '../Field'
import { Input } from '../Input'
import { SegmentedControl } from '../SegmentedControl'
import { Select } from '../Select'
import { Switch } from '../Switch'
import { Textarea } from '../Textarea'
import { installDomPolyfills, switchToEnglish } from './test-helpers'

beforeAll(async () => {
  installDomPolyfills()
  await switchToEnglish()
})

describe('Input', () => {
  it('forwards props and marks invalid', () => {
    const onChange = vi.fn()
    render(<Input aria-label="Name" invalid onChange={onChange} />)
    const input = screen.getByRole('textbox', { name: 'Name' })
    expect(input).toHaveAttribute('aria-invalid', 'true')
    fireEvent.change(input, { target: { value: 'abc' } })
    expect(onChange).toHaveBeenCalled()
  })
})

describe('Textarea', () => {
  it('renders a multi-line control', () => {
    render(<Textarea aria-label="Prompt" defaultValue="hi" />)
    const area = screen.getByRole('textbox', { name: 'Prompt' })
    expect(area.tagName).toBe('TEXTAREA')
    expect(area).toHaveValue('hi')
  })
})

describe('Select', () => {
  it('renders options and a disabled placeholder', () => {
    const onChange = vi.fn()
    render(
      <Select aria-label="Model" value="" onChange={onChange} placeholder="Pick one"
        options={[{ value: 'a', label: 'Alpha' }, { value: 'b', label: 'Beta' }]} />,
    )
    const select = screen.getByRole('combobox', { name: 'Model' })
    expect(screen.getByRole('option', { name: 'Pick one' })).toBeDisabled()
    fireEvent.change(select, { target: { value: 'b' } })
    expect(onChange).toHaveBeenCalled()
  })
})

describe('Field', () => {
  it('labels the control and links helper text', () => {
    render(<Field label="API key" helper="Stored encrypted"><Input /></Field>)
    const input = screen.getByLabelText('API key')
    expect(input).toHaveAccessibleDescription('Stored encrypted')
    expect(input).not.toHaveAttribute('aria-invalid')
  })

  it('shows the error with role=alert, hides helper and marks the control invalid', () => {
    render(<Field label="API key" helper="Stored encrypted" error="API key is required"><Input /></Field>)
    const input = screen.getByLabelText('API key')
    expect(screen.getByRole('alert')).toHaveTextContent('API key is required')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(input).toHaveAccessibleDescription('API key is required')
    expect(screen.queryByText('Stored encrypted')).toBeNull()
  })

  it('keeps an explicit child id', () => {
    render(<Field label="Name"><Input id="agent-name" /></Field>)
    expect(screen.getByLabelText('Name')).toHaveAttribute('id', 'agent-name')
  })
})

describe('Switch', () => {
  it('exposes role=switch with aria-checked and toggles', () => {
    function Harness() {
      const [on, setOn] = useState(false)
      return <Switch aria-label="Enabled" checked={on} onCheckedChange={setOn} />
    }
    render(<Harness />)
    const sw = screen.getByRole('switch', { name: 'Enabled' })
    expect(sw).toHaveAttribute('aria-checked', 'false')
    fireEvent.click(sw)
    expect(sw).toHaveAttribute('aria-checked', 'true')
  })

  it('does not toggle when disabled', () => {
    const onChange = vi.fn()
    render(<Switch aria-label="Off" checked={false} onCheckedChange={onChange} disabled />)
    fireEvent.click(screen.getByRole('switch', { name: 'Off' }))
    expect(onChange).not.toHaveBeenCalled()
  })
})

describe('Checkbox', () => {
  it('reports changes and supports an inline label', () => {
    const onChange = vi.fn()
    render(<Checkbox label="Remember" checked={false} onCheckedChange={onChange} />)
    fireEvent.click(screen.getByRole('checkbox', { name: 'Remember' }))
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('shows the mixed state', () => {
    render(<Checkbox aria-label="All" checked={false} indeterminate onCheckedChange={() => {}} />)
    const box = screen.getByRole('checkbox', { name: 'All' }) as HTMLInputElement
    expect(box.indeterminate).toBe(true)
    expect(box).toHaveAttribute('aria-checked', 'mixed')
  })
})

describe('SegmentedControl', () => {
  function Harness({ onChange }: { onChange?: (v: string) => void }) {
    const [value, setValue] = useState<'system' | 'light' | 'dark'>('system')
    return (
      <SegmentedControl
        aria-label="Theme"
        value={value}
        onValueChange={(v) => { setValue(v); onChange?.(v) }}
        options={[
          { value: 'system', label: 'System' },
          { value: 'light', label: 'Light' },
          { value: 'dark', label: 'Dark' },
        ]}
      />
    )
  }

  it('is a radiogroup with one checked radio', () => {
    render(<Harness />)
    expect(screen.getByRole('radiogroup', { name: 'Theme' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: 'System' })).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByRole('radio', { name: 'Light' })).toHaveAttribute('tabindex', '-1')
  })

  it('selects on click and moves with arrow keys', () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    fireEvent.click(screen.getByRole('radio', { name: 'Dark' }))
    expect(onChange).toHaveBeenLastCalledWith('dark')
    fireEvent.keyDown(screen.getByRole('radio', { name: 'Dark' }), { key: 'ArrowRight' })
    expect(onChange).toHaveBeenLastCalledWith('system')
    expect(screen.getByRole('radio', { name: 'System' })).toHaveFocus()
    fireEvent.keyDown(screen.getByRole('radio', { name: 'System' }), { key: 'ArrowLeft' })
    expect(onChange).toHaveBeenLastCalledWith('dark')
  })
})
