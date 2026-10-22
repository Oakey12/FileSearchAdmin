// Фон окна: обычный или одна из сцен. Интерфейс поверх сцены — матовое стекло.
//   «Частицы» — ночное небо с сиянием и размытыми светящимися кругами
//               (по мотивам codepen.io/hf666/pen/WVrpWe);
//   «Лес»     — тёмная лесная ночь и размытые мигающие светлячки
//               (по мотивам codepen.io/mikegolus/pen/Jegvym).
//
// Скорость важнее эффекта: кругов немного, анимируются только transform и
// opacity, у всех один @keyframes с CSS-переменными, на свёрнутом окне
// анимация стоит, а без фона элементов нет вовсе.

export type Backdrop = 'none' | 'particles' | 'forest'

const SCENES: Backdrop[] = ['particles', 'forest']

function asBackdrop(v: string | null | undefined): Backdrop {
  return SCENES.includes(v as Backdrop) ? (v as Backdrop) : 'none'
}

/** Ключ в localStorage. Тот же ключ читает встроенный скрипт в index.html. */
export const BACKDROP_KEY = 'backdrop'

/** Сколько кругов. В оригинале 200; здесь — немного, но крупных. */
export const PARTICLE_COUNT = 22

type Store = Pick<Storage, 'getItem' | 'setItem'>
const defaultStore = (): Store => localStorage

export function loadBackdrop(store: () => Store = defaultStore): Backdrop {
  try {
    return asBackdrop(store().getItem(BACKDROP_KEY))
  } catch {
    return 'none'
  }
}

export function saveBackdrop(value: Backdrop, store: () => Store = defaultStore): void {
  try { store().setItem(BACKDROP_KEY, value) } catch { /* без хранилища — на один запуск */ }
}

/** Ставит фон на <html>: от атрибута зависят прозрачность панелей и цвета. */
export function applyBackdrop(value: Backdrop, root: HTMLElement = document.documentElement): void {
  if (value === 'none') delete root.dataset.backdrop
  else root.dataset.backdrop = value
}

export function currentBackdrop(root: HTMLElement = document.documentElement): Backdrop {
  return asBackdrop(root.dataset.backdrop)
}

/** Параметры одного светлячка: всё случайное, чтобы небо не повторялось. */
export interface Particle {
  size: number // px, 18–58
  path: [number, number][] // 4 точки (vw, vh): светлячок кружит между ними и возвращается
  duration: number // мс — полный круг по точкам
  delay: number // мс, отрицательная: с первого кадра светлячки уже в разных местах пути
  pulse: number // мс — период мерцания
  pulseDelay: number // мс
  blur: number // px — глубина: дальние мягче, ближние чётче
}

/** Случайные светлячки. rnd — для тестов; по умолчанию Math.random. */
export function makeParticles(count = PARTICLE_COUNT, rnd: () => number = Math.random): Particle[] {
  const between = (a: number, b: number) => a + (b - a) * rnd()
  return Array.from({ length: count }, () => {
    const duration = between(45000, 90000) // медленно: полный круг за минуту-полторы
    // Точки — по всему окну, в разные стороны, а не только снизу вверх.
    const path = Array.from({ length: 4 }, (): [number, number] => [between(-4, 104), between(-4, 104)])
    const near = rnd() // 0 — далеко, 1 — близко
    return {
      size: Math.round(18 + 40 * near ** 2),
      path,
      duration,
      delay: -between(0, duration),
      pulse: between(3500, 7500),
      pulseDelay: -between(0, 7500),
      blur: Math.round((3 + 6 * (1 - near)) * 10) / 10,
    }
  })
}

/** Рисует светлячков в host; возвращает функцию, которая всё убирает. */
export function mountParticles(host: HTMLElement, particles: Particle[] = makeParticles()): () => void {
  // «Уменьшение движения» в Windows здесь не учитываем: фон человек включил сам,
  // а в удалённом рабочем столе и на офисных ПК анимации Windows часто выключены.
  const frag = document.createDocumentFragment()
  for (const p of particles) {
    const wrap = document.createElement('span')
    wrap.className = 'bd-particle'
    const s = wrap.style
    s.width = s.height = `${p.size}px`
    p.path.forEach(([x, y], i) => {
      s.setProperty(`--x${i}`, `${x.toFixed(1)}vw`)
      s.setProperty(`--y${i}`, `${y.toFixed(1)}vh`)
    })
    s.animationDuration = `${Math.round(p.duration)}ms`
    s.animationDelay = `${Math.round(p.delay)}ms`
    const dot = document.createElement('span')
    dot.className = 'bd-dot'
    dot.style.filter = `blur(${p.blur}px)`
    dot.style.animationDuration = `${Math.round(p.pulse)}ms`
    dot.style.animationDelay = `${Math.round(p.pulseDelay)}ms`
    wrap.append(dot)
    frag.append(wrap)
  }
  host.replaceChildren(frag)

  return pauseWhenHidden(host, () => host.replaceChildren())
}

/** Окно свёрнуто или закрыто другим — анимация в host не нужна. */
function pauseWhenHidden(host: HTMLElement, cleanup: () => void): () => void {
  const onVisibility = () => host.classList.toggle('bd-paused', document.hidden)
  document.addEventListener('visibilitychange', onVisibility)
  onVisibility()
  return () => {
    document.removeEventListener('visibilitychange', onVisibility)
    cleanup()
    host.classList.remove('bd-paused')
  }
}

/**
 * Следит за плавностью первые две секунды после включения фона: если кадры
 * медленнее 45 в секунду (слабая видеокарта, удалённый рабочий стол), вызывает
 * onSlow — стекло без размытия. Возвращает отмену.
 */
export function watchSmoothness(onSlow: () => void, sampleMs = 2000, minFps = 45): () => void {
  let frames = 0
  let start = 0
  let raf = requestAnimationFrame(function step(t) {
    if (!start) start = t
    frames++
    if (t - start < sampleMs) {
      raf = requestAnimationFrame(step)
    } else if (!document.hidden && (frames * 1000) / (t - start) < minFps) {
      onSlow()
    }
  })
  return () => cancelAnimationFrame(raf)
}

// --- Лес -----------------------------------------------------------------------

/** Сколько светлячков в лесу. В оригинале 15. */
export const FIREFLY_COUNT = 18

/** Параметры светлячка в лесу. */
export interface Firefly {
  path: [number, number][] // 8 точек (vw, vh): медленный путь туда и обратно
  duration: number // мс — путь в одну сторону
  delay: number // мс, отрицательная
  orbit: number // vw — радиус петель вокруг пути
  orbitDuration: number // мс
  flash: number // мс — период вспышек
  flashDelay: number // мс
  size: number // px — свечение
  blur: number // px — размытие: светлячки мягкие, на разной глубине
}

export function makeFireflies(count = FIREFLY_COUNT, rnd: () => number = Math.random): Firefly[] {
  const between = (a: number, b: number) => a + (b - a) * rnd()
  return Array.from({ length: count }, () => {
    const duration = between(60000, 120000)
    const flash = between(5000, 11000)
    return {
      path: Array.from({ length: 8 }, (): [number, number] => [between(-2, 102), between(8, 100)]),
      duration,
      delay: -between(0, duration),
      orbit: Math.round(between(2, 7) * 10) / 10,
      orbitDuration: between(8000, 18000),
      flash,
      flashDelay: -between(0, flash),
      size: Math.round(between(56, 96)),
      blur: Math.round(between(1.5, 5) * 10) / 10,
    }
  })
}

/** Рисует светлячков; возвращает функцию, которая всё убирает. */
export function mountForest(scene: HTMLElement, host: HTMLElement, flies: Firefly[] = makeFireflies()): () => void {
  const frag = document.createDocumentFragment()
  for (const f of flies) {
    const fly = document.createElement('span')
    fly.className = 'ff'
    const s = fly.style
    f.path.forEach(([x, y], i) => {
      s.setProperty(`--x${i}`, `${x.toFixed(1)}vw`)
      s.setProperty(`--y${i}`, `${y.toFixed(1)}vh`)
    })
    s.animationDuration = `${Math.round(f.duration)}ms`
    s.animationDelay = `${Math.round(f.delay)}ms`
    const orbit = document.createElement('span')
    orbit.className = 'ff-orbit'
    orbit.style.setProperty('--r', `${f.orbit}vw`)
    orbit.style.animationDuration = `${Math.round(f.orbitDuration)}ms`
    const light = document.createElement('span')
    light.className = 'ff-light'
    light.style.width = light.style.height = `${f.size}px`
    light.style.filter = `blur(${f.blur}px)` // один раз, не анимируется
    light.style.animationDuration = `${Math.round(f.flash)}ms`
    light.style.animationDelay = `${Math.round(f.flashDelay)}ms`
    orbit.append(light)
    fly.append(orbit)
    frag.append(fly)
  }
  host.replaceChildren(frag)
  return pauseWhenHidden(scene, () => host.replaceChildren())
}
