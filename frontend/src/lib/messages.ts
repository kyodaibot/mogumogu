// User-facing (Chinese) text for API error codes and validation reasons.
// The API's own `message` is for developers and is never shown.

import { ApiError } from '../api'

const codeMessages: Record<string, string> = {
  invalid_credentials: '用户名或密码错误。',
  username_taken: '这个用户名已经被使用了。',
  rate_limited: '尝试次数过多，请稍后再试。',
  csrf_failed: '安全校验失败，请刷新页面后重试。',
  unauthenticated: '登录已失效，请重新登录。',
  not_found: '记录不存在或已被删除。',
  profile_not_found: '还没有填写 Profile。',
  payload_too_large: '提交的内容太长。',
  validation_failed: '请检查标出的项目。',
  network_error: '无法连接服务器，请检查网络或稍后重试。',
  server_unavailable: '服务器暂时不可用，请稍后重试。',
  internal_error: '服务器出错，请稍后重试。',
}

const reasonMessages: Record<string, string> = {
  required: '必填',
  too_short: '太短',
  too_long: '太长',
  out_of_range: '超出允许范围',
  too_many_decimals: '最多 1 位小数',
  invalid_format: '格式不正确',
  invalid_value: '请选择有效的选项',
  duplicate_item: '有重复项',
  must_be_after_starts_at: '结束时间必须晚于开始时间',
  duration_too_long: '行程不能超过 24 小时',
  range_too_large: '日期范围太大',
  must_not_be_before_from: '结束日期不能早于开始日期',
}

// More specific wording for some field/reason pairs.
const fieldReasonMessages: Record<string, string> = {
  'username/too_short': '至少 3 个字符',
  'username/too_long': '最多 32 个字符',
  'username/invalid_format': '只能使用字母、数字和下划线',
  'password/too_short': '至少 8 个字符',
  'password/too_long': '最多 128 个字符',
  'height_cm/out_of_range': '请输入 50–300 之间的数值',
  'weight_kg/out_of_range': '请输入 10–500 之间的数值',
  'age/out_of_range': '请输入 10–120 之间的整数',
  'allergens/too_long': '最多 20 项',
  'title/too_long': '最多 100 个字符',
  'notes/too_long': '最多 1000 个字符',
  'content/too_long': '最多 200 个字符',
  'calories/out_of_range': '请输入 0–10000 之间的数值',
  'protein_g/out_of_range': '请输入 0–1000 之间的数值',
  'fat_g/out_of_range': '请输入 0–1000 之间的数值',
  'carbs_g/out_of_range': '请输入 0–1000 之间的数值',
}

/** A general message for any error. */
export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'rate_limited') return codeMessages.rate_limited
    return codeMessages[err.code] ?? `请求失败（${err.status}）。`
  }
  return '发生了意外错误。'
}

/**
 * Field messages from a validation error, keyed by field name. Array items
 * such as "allergens[2]" are reported on their array field ("allergens").
 */
export function fieldMessages(err: unknown): Record<string, string> {
  const out: Record<string, string> = {}
  if (!(err instanceof ApiError) || err.code !== 'validation_failed') return out
  for (const d of err.details) {
    const field = d.field.replace(/\[\d+\]$/, '')
    const item = d.field !== field ? `第 ${Number(d.field.match(/\[(\d+)\]$/)![1]) + 1} 项：` : ''
    const text = fieldReasonMessages[`${field}/${d.reason}`] ?? reasonMessages[d.reason] ?? d.reason
    out[field] = out[field] ? `${out[field]}；${item}${text}` : item + text
  }
  return out
}
