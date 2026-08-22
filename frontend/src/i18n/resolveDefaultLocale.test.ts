import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { BUILT_IN_DEFAULT, resolveDefaultLocale } from './resolveDefaultLocale.ts'

describe('resolveDefaultLocale', () => {
  test('runtime locale wins over build-time and built-in default', () => {
    assert.equal(resolveDefaultLocale('zh-CN', 'ko-KR'), 'zh-CN')
    assert.equal(resolveDefaultLocale('vi-VN', 'en-US'), 'vi-VN')
  })

  test('build-time locale applies when runtime is missing', () => {
    assert.equal(resolveDefaultLocale(null, 'ru-RU'), 'ru-RU')
    assert.equal(resolveDefaultLocale(undefined, 'ko-KR'), 'ko-KR')
    assert.equal(resolveDefaultLocale('', 'zh-CN'), 'zh-CN')
  })

  test('falls back to the built-in default when nothing is provided', () => {
    assert.equal(resolveDefaultLocale(), BUILT_IN_DEFAULT)
    assert.equal(resolveDefaultLocale(null, null), BUILT_IN_DEFAULT)
    assert.equal(resolveDefaultLocale(undefined, ''), BUILT_IN_DEFAULT)
  })

  test('trims whitespace around the candidate', () => {
    assert.equal(resolveDefaultLocale('  en-US  '), 'en-US')
  })

  test('unsupported values fall back to the built-in default', () => {
    assert.equal(resolveDefaultLocale('fr-FR', 'de-DE'), BUILT_IN_DEFAULT)
    assert.equal(resolveDefaultLocale('en'), BUILT_IN_DEFAULT)
    assert.equal(resolveDefaultLocale(''), BUILT_IN_DEFAULT)
  })

  test('every supported locale resolves to itself', () => {
    for (const locale of ['zh-CN', 'en-US', 'ru-RU', 'ko-KR', 'vi-VN'] as const) {
      assert.equal(resolveDefaultLocale(locale), locale)
    }
  })
})