import { describe, expect, it } from 'vitest'
import { sourceLinks } from './sources'

describe('sourceLinks', () => {
  it('labels a link with its title, or its domain without one', () => {
    expect(
      sourceLinks([
        { title: ' メニュー ', url: 'https://example.com/menu' },
        { title: '', url: 'https://www.kyoto-u.ac.jp/ja/access/coop' },
      ]),
    ).toEqual([
      { href: 'https://example.com/menu', label: 'メニュー' },
      { href: 'https://www.kyoto-u.ac.jp/ja/access/coop', label: 'kyoto-u.ac.jp' },
    ])
  })

  it('keeps http and https links only', () => {
    const links = sourceLinks([
      { title: 'a', url: 'javascript:alert(1)' },
      { title: 'b', url: 'JAVASCRIPT:alert(1)' },
      { title: 'c', url: 'data:text/html,hi' },
      { title: 'd', url: 'not a url' },
      { title: 'e', url: 'http://example.com/' },
    ])
    expect(links.map((l) => l.label)).toEqual(['e'])
  })
})
