import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { lightTheme } from './themes'

type RGB = [number, number, number]

function rgb(hex: string): RGB {
  return [1, 3, 5].map(index => parseInt(hex.slice(index, index + 2), 16)) as RGB
}

function composite(foreground: RGB, background: RGB, opacity: number): RGB {
  return foreground.map((value, index) => value * opacity + background[index]! * (1 - opacity)) as RGB
}

// WCAG relative luminance uses linearized sRGB, not the average RGB value.
function luminance(color: RGB): number {
  const linear = color.map(value => {
    const channel = value / 255
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  })
  return linear[0]! * 0.2126 + linear[1]! * 0.7152 + linear[2]! * 0.0722
}

function contrast(foreground: RGB, background: RGB): number {
  const values = [luminance(foreground), luminance(background)].sort((a, b) => a - b)
  return (values[1]! + 0.05) / (values[0]! + 0.05)
}

const colors: Record<string, string> = lightTheme.colors
const css = readFileSync(new URL('../styles/app-retro.css', import.meta.url), 'utf8')
const cockpit = readFileSync(new URL('../views/CockpitView.vue', import.meta.url), 'utf8')
const applicationBackground = css.match(/\.v-application\s*\{[^}]*background-color:\s*(#[0-9a-f]{6})/i)![1]!
const backgrounds = [colors.surface!, colors.background!, colors['surface-variant']!, applicationBackground]

describe('浅色主题正文的 WCAG AA 对比度', () => {
  it.each(['info', 'success', 'warning', 'error'])('%s 文字在实际浅色表面和 tonal 卡片上至少为 4.5:1', name => {
    const foreground = rgb(colors[name]!)
    for (const value of backgrounds) {
      const background = rgb(value)
      expect(contrast(foreground, background), `${name} on ${value}`).toBeGreaterThanOrEqual(4.5)
      // Vuetify tonal cards composite their semantic color at activated opacity (12%).
      const tonal = composite(foreground, background, 0.12)
      const caption = composite(rgb(colors['on-background']!), tonal, Number(lightTheme.variables!['high-emphasis-opacity']))
      expect(contrast(caption, tonal), `${name} caption on tonal ${value}`).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('普通次级说明按实际 opacity 合成后仍达到正文 4.5:1', () => {
    const opacity = Number(lightTheme.variables!['medium-emphasis-opacity'])
    for (const value of backgrounds) {
      const background = rgb(value)
      const foreground = composite(rgb(colors['on-background']!), background, opacity)
      expect(contrast(foreground, background), `medium emphasis on ${value}`).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('仪表盘 caption 的强调类不会再次把语义文字淡化到 4.5:1 以下', () => {
    const captions = [...cockpit.matchAll(/class="([^"]*\btext-caption\b[^"]*)"/g)]
    expect(captions.length).toBeGreaterThan(0)
    for (const [, classes] of captions) {
      const emphasis = classes!.includes('text-medium-emphasis') ? 'medium' : 'high'
      const opacity = Number(lightTheme.variables![`${emphasis}-emphasis-opacity`])
      for (const name of ['info', 'success', 'warning', 'error']) {
        const foreground = rgb(colors[name]!)
        const tonal = composite(foreground, rgb(colors.surface!), 0.12)
        expect(contrast(composite(rgb(colors['on-background']!), tonal, opacity), tonal), `${classes} on ${name} card`)
          .toBeGreaterThanOrEqual(4.5)
      }
    }
  })

  it.each(['info', 'success', 'emerald', 'error'])('统计卡 %s 数值的 CSS 覆盖也符合正文对比度', name => {
    const rule = css.match(new RegExp(`\\.stat-card-${name} \\.stat-card-value\\s*\\{[^}]*color:\\s*(#[0-9a-f]{6})`, 'i'))
    expect(rule).not.toBeNull()
    const foreground = rgb(rule![1]!)
    for (const value of backgrounds) {
      expect(contrast(foreground, rgb(value)), `${name} stat value on ${value}`).toBeGreaterThanOrEqual(4.5)
    }
  })
})
