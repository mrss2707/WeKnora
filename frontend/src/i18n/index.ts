import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN.ts'
import ruRU from './locales/ru-RU.ts'
import enUS from './locales/en-US.ts'
import koKR from './locales/ko-KR.ts'
import viVN from './locales/vi-VN.ts'

const messages = {
  'zh-CN': zhCN,
  'en-US': enUS,
  'ru-RU': ruRU,
  'ko-KR': koKR,
  'vi-VN': viVN
}

// Lấy ngôn ngữ đã lưu từ localStorage hoặc sử dụng tiếng Việt làm mặc định
const savedLocale = localStorage.getItem('locale') || 'vi-VN'

const i18n = createI18n({
  legacy: false,
  locale: savedLocale,
  fallbackLocale: 'vi-VN',
  globalInjection: true,
  // Some translations intentionally embed `<strong>` markup (e.g. agent step summaries).
  // We render them via v-html with our own sanitization, so silence vue-i18n's HTML warning
  // to avoid flooding the console and slowing renders during history loads.
  warnHtmlMessage: false,
  messages
})

export default i18n