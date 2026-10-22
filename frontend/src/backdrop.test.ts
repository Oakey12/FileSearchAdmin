// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { BACKDROP_KEY, FIREFLY_COUNT, PARTICLE_COUNT, applyBackdrop, currentBackdrop, loadBackdrop, makeFireflies, makeParticles, mountForest, mountParticles, saveBackdrop, watchSmoothness } from './backdrop'

function memoryStore() {
  const data = new Map<string, string>()
  return {
    getItem: (k: string) => data.get(k) ?? null,
    setItem: (k: string, v: string) => { data.set(k, v) },
    data,
  }
}

// Предсказуемый генератор для проверок.
function seeded(seed: number): () => number {
  let s = seed
  return () => {
    s = (s * 16807) % 2147483647
    return (s - 1) / 2147483646
  }
}

describe('выбор фона', () => {
  it('по умолчанию — без фона, выбор сохраняется', () => {
    const store = memoryStore()
    expect(loadBackdrop(() => store)).toBe('none')
    saveBackdrop('particles', () => store)
    expect(store.data.get(BACKDROP_KEY)).toBe('particles')
    expect(loadBackdrop(() => store)).toBe('particles')
  })

  it('недоступное хранилище не ломает программу', () => {
    const broken = () => { throw new Error('нет доступа') }
    expect(loadBackdrop(broken)).toBe('none')
    expect(() => saveBackdrop('particles', broken)).not.toThrow()
  })

  it('атрибут на <html> ставится и снимается', () => {
    const root = document.createElement('html')
    applyBackdrop('particles', root)
    expect(root.dataset.backdrop).toBe('particles')
    expect(currentBackdrop(root)).toBe('particles')
    applyBackdrop('none', root)
    expect(root.dataset.backdrop).toBeUndefined()
    expect(currentBackdrop(root)).toBe('none')
  })
})

describe('частицы', () => {
  it('крупные, размытые, летают по всему окну в разные стороны', () => {
    const ps = makeParticles(400, seeded(7))
    expect(ps).toHaveLength(400)
    for (const p of ps) {
      expect(p.size).toBeGreaterThanOrEqual(18)
      expect(p.size).toBeLessThanOrEqual(58)
      expect(p.blur).toBeGreaterThanOrEqual(3)
      expect(p.blur).toBeLessThanOrEqual(9)
      expect(p.duration).toBeGreaterThanOrEqual(45000) // медленно
      expect(p.path).toHaveLength(4)
      // Отрицательная задержка: с первого кадра светлячок уже в пути.
      expect(p.delay).toBeLessThanOrEqual(0)
      expect(-p.delay).toBeLessThanOrEqual(p.duration)
    }
    // Точки — по всему окну, а не кучкой.
    const xs = ps.flatMap((p) => p.path.map(([x]) => x))
    const ys = ps.flatMap((p) => p.path.map(([, y]) => y))
    expect(Math.min(...xs)).toBeLessThan(5)
    expect(Math.max(...xs)).toBeGreaterThan(95)
    expect(Math.min(...ys)).toBeLessThan(5)
    expect(Math.max(...ys)).toBeGreaterThan(95)
    // В разные стороны: есть и движение вниз, и влево, а не только вверх.
    const steps = ps.flatMap((p) => p.path.map(([x, y], i) => [p.path[(i + 1) % 4][0] - x, p.path[(i + 1) % 4][1] - y]))
    expect(steps.some(([, dy]) => dy > 20)).toBe(true)
    expect(steps.some(([, dy]) => dy < -20)).toBe(true)
    expect(steps.some(([dx]) => dx < -20)).toBe(true)
    expect(steps.some(([dx]) => dx > 20)).toBe(true)
  })

  it('рисуются, ставятся на паузу на скрытом окне и убираются без следа', () => {
    const host = document.createElement('div')
    const stop = mountParticles(host, makeParticles(PARTICLE_COUNT, seeded(3)))
    expect(host.querySelectorAll('.bd-particle')).toHaveLength(PARTICLE_COUNT)
    expect(host.querySelectorAll('.bd-dot')).toHaveLength(PARTICLE_COUNT)
    const first = host.querySelector<HTMLElement>('.bd-particle')!
    expect(first.style.getPropertyValue('--x3')).toMatch(/vw$/)
    expect(first.style.getPropertyValue('--y3')).toMatch(/vh$/)
    expect(host.querySelector<HTMLElement>('.bd-dot')!.style.filter).toMatch(/^blur\(/)
    expect(first.style.animationDelay.startsWith('-')).toBe(true)

    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(host.classList.contains('bd-paused')).toBe(true)

    stop()
    expect(host.childElementCount).toBe(0)
    expect(host.classList.contains('bd-paused')).toBe(false)
    // После остановки обработчик снят: класс больше не появляется.
    document.dispatchEvent(new Event('visibilitychange'))
    expect(host.classList.contains('bd-paused')).toBe(false)
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
  })
})

describe('плавность', () => {
  afterEach(() => vi.unstubAllGlobals())

  // Кадры с заданным шагом: requestAnimationFrame сразу зовёт следующий кадр.
  function runFrames(stepMs: number, onSlow: () => void): void {
    let t = 0
    const queue: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => queue.push(cb))
    vi.stubGlobal('cancelAnimationFrame', () => {})
    watchSmoothness(onSlow)
    while (queue.length) {
      t += stepMs
      queue.shift()!(t)
    }
  }

  it('на быстром компьютере стекло остаётся с размытием', () => {
    const slow = vi.fn()
    runFrames(16, slow) // ~60 кадров в секунду
    expect(slow).not.toHaveBeenCalled()
  })

  it('на медленном — размытие отключается', () => {
    const slow = vi.fn()
    runFrames(40, slow) // 25 кадров в секунду
    expect(slow).toHaveBeenCalledOnce()
  })
})

describe('лес', () => {
  it('выбор «Лес» сохраняется, неизвестное значение — без фона', () => {
    const store = memoryStore()
    saveBackdrop('forest', () => store)
    expect(loadBackdrop(() => store)).toBe('forest')
    store.setItem(BACKDROP_KEY, 'что-то старое')
    expect(loadBackdrop(() => store)).toBe('none')
    const root = document.createElement('html')
    applyBackdrop('forest', root)
    expect(currentBackdrop(root)).toBe('forest')
  })

  it('светлячки летают по всему окну, вспыхивают в разное время', () => {
    const flies = makeFireflies(300, seeded(11))
    for (const f of flies) {
      expect(f.path).toHaveLength(8)
      expect(f.delay).toBeLessThanOrEqual(0)
      expect(f.flash).toBeGreaterThanOrEqual(5000)
      expect(f.flash).toBeLessThanOrEqual(11000)
      expect(-f.flashDelay).toBeLessThanOrEqual(f.flash)
    }
    const xs = flies.flatMap((f) => f.path.map(([x]) => x))
    expect(Math.min(...xs)).toBeLessThan(5)
    expect(Math.max(...xs)).toBeGreaterThan(95)
    // Вспышки не одновременные.
    expect(new Set(flies.map((f) => Math.round(f.flashDelay / 500))).size).toBeGreaterThan(10)
  })

  it('рисуется и убирается без следа', () => {
    const scene = document.createElement('div')
    const host = document.createElement('div')
    const stop = mountForest(scene, host, makeFireflies(FIREFLY_COUNT, seeded(2)))
    expect(host.querySelectorAll('.ff')).toHaveLength(FIREFLY_COUNT)
    expect(host.querySelector<HTMLElement>('.ff-light')!.style.filter).toMatch(/^blur/)
    expect(host.querySelectorAll('.ff-light')).toHaveLength(FIREFLY_COUNT)
    expect(host.querySelector<HTMLElement>('.ff')!.style.getPropertyValue('--x7')).toMatch(/vw$/)
    stop()
    expect(host.childElementCount).toBe(0)
  })
})
