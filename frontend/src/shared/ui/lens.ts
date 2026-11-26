// Edge refraction for glass. The backdrop behind an element is pushed through
// an SVG displacement map shaped like the element: flat in the middle, bending
// inward along a rounded bezel, as a thick piece of glass does. Only Chromium
// accepts an SVG filter in backdrop-filter; other browsers keep the blur and
// saturation of the .glass class.
import { useLayoutEffect, useRef } from 'react';

const svgNamespace = 'http://www.w3.org/2000/svg';
const defsId = 'glass-lens-defs';
let filterCount = 0;

export interface LensBox {
  width: number;
  height: number;
  radius: number;
  bezel: number;
}

// lensDisplacement says, for the pixel at (x, y) of a rounded rectangle, from
// which direction and how far (0 to 1 of the full depth) the backdrop is
// sampled. Positive x samples from the right, positive y from below. Pixels in
// the flat middle and outside the rounded corners are not moved.
export function lensDisplacement(x: number, y: number, box: LensBox) {
  const { bezel } = box;
  const { px, py, qx, qy, inside } = edgeDistance(x, y, box);
  if (inside < 0 || inside >= bezel) return { x: 0, y: 0 };

  // The outward normal of the nearest edge.
  let nx = 0;
  let ny = 0;
  if (qx > 0 && qy > 0) {
    const length = Math.hypot(qx, qy) || 1;
    nx = (qx / length) * Math.sign(px);
    ny = (qy / length) * Math.sign(py);
  } else if (qx > qy) {
    nx = Math.sign(px);
  } else {
    ny = Math.sign(py);
  }

  // A circular bezel profile: steepest at the rim, flat where it meets the middle.
  const t = 1 - inside / bezel;
  const amount = 1 - Math.sqrt(1 - t * t);
  return { x: -nx * amount || 0, y: -ny * amount || 0 };
}

// edgeDistance places the pixel at (x, y) in a rounded rectangle: its offset
// from the centre, how far it lies past the straight edges, and its distance
// inside the rounded edge (negative outside it).
function edgeDistance(x: number, y: number, { width, height, radius }: LensBox) {
  const halfX = width / 2;
  const halfY = height / 2;
  const px = x + 0.5 - halfX;
  const py = y + 0.5 - halfY;
  const qx = Math.abs(px) - (halfX - radius);
  const qy = Math.abs(py) - (halfY - radius);
  const inside = radius - Math.hypot(Math.max(qx, 0), Math.max(qy, 0)) - Math.min(Math.max(qx, qy), 0);
  return { px, py, qx, qy, inside };
}

// rimFeather is how far, in pixels, the sharp rim fades into the blurred
// middle past the bezel.
const rimFeather = 6;

// rimWeight says how much of the pixel at (x, y) shows the sharp, bent rim
// rather than the blurred middle: 1 across the bezel (and outside the shape,
// which the element clips), fading to 0 over rimFeather pixels past it.
export function rimWeight(x: number, y: number, box: LensBox) {
  const { inside } = edgeDistance(x, y, box);
  if (inside < box.bezel) return 1;
  return Math.max(0, 1 - (inside - box.bezel) / rimFeather);
}

// supportsLensRefraction reports whether the browser is Chromium, the only
// engine that applies an SVG filter as a backdrop filter, headless included.
// Chrome on iOS is WebKit and names itself CriOS, so it is refused.
export function supportsLensRefraction(userAgent: string) {
  return /Chrome\/\d/.test(userAgent);
}

// displacementMap draws lensDisplacement into an image the SVG filter reads:
// red is x, green is y, 128 is no displacement. Blue is rimWeight, the mask a
// sharp-rim lens blends its bent rim with its blurred middle by.
function displacementMap(box: LensBox) {
  const canvas = document.createElement('canvas');
  canvas.width = box.width;
  canvas.height = box.height;
  const context = canvas.getContext('2d');
  if (!context) return '';
  const image = context.createImageData(box.width, box.height);
  for (let y = 0; y < box.height; y++) {
    for (let x = 0; x < box.width; x++) {
      const shift = lensDisplacement(x, y, box);
      const index = (y * box.width + x) * 4;
      image.data[index] = 128 + shift.x * 127;
      image.data[index + 1] = 128 + shift.y * 127;
      image.data[index + 2] = rimWeight(x, y, box) * 255;
      image.data[index + 3] = 255;
    }
  }
  context.putImageData(image, 0, 0);
  return canvas.toDataURL();
}

function filterDefs() {
  let svg = document.getElementById(defsId) as SVGSVGElement | null;
  if (!svg) {
    svg = document.createElementNS(svgNamespace, 'svg');
    svg.id = defsId;
    svg.setAttribute('width', '0');
    svg.setAttribute('height', '0');
    svg.setAttribute('aria-hidden', 'true');
    svg.style.position = 'absolute';
    document.body.appendChild(svg);
  }
  return svg;
}

function refractionWanted() {
  if (typeof window === 'undefined' || !supportsLensRefraction(navigator.userAgent)) return false;
  return !window.matchMedia('(prefers-reduced-transparency: reduce)').matches;
}

export interface LensOptions {
  // bezel is the width of the bending rim in pixels, at most the corner radius.
  bezel?: number;
  // depth is how far, in pixels, the backdrop is pulled in at the rim.
  depth?: number;
  // blur is the blur across the glass; rimBlur, when given, is a lighter one
  // for the bezel alone, so dense glass keeps a blurred middle to read on
  // while its rim still shows the backdrop bending.
  blur?: number;
  rimBlur?: number;
  saturate?: number;
  // tone is CSS filter functions applied to the backdrop after the lens, such
  // as a custom property that lifts it in one theme.
  tone?: string;
}

// useLens returns a ref that gives its element edge refraction in Chromium.
// The map follows the element's size and corner radius as they change.
export function useLens<T extends HTMLElement>({ bezel = 14, depth = 22, blur = 3, rimBlur, saturate = 170, tone }: LensOptions = {}) {
  const ref = useRef<T>(null);

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element || !refractionWanted()) return;

    const id = `glass-lens-${++filterCount}`;
    const filter = document.createElementNS(svgNamespace, 'filter');
    filter.id = id;
    filter.setAttribute('x', '0');
    filter.setAttribute('y', '0');
    filter.setAttribute('filterUnits', 'userSpaceOnUse');
    filter.setAttribute('primitiveUnits', 'userSpaceOnUse');
    filter.setAttribute('color-interpolation-filters', 'sRGB');
    const map = document.createElementNS(svgNamespace, 'feImage');
    map.setAttribute('x', '0');
    map.setAttribute('y', '0');
    map.setAttribute('preserveAspectRatio', 'none');
    map.setAttribute('result', 'map');
    const displace = document.createElementNS(svgNamespace, 'feDisplacementMap');
    displace.setAttribute('in', 'SourceGraphic');
    displace.setAttribute('in2', 'map');
    displace.setAttribute('xChannelSelector', 'R');
    displace.setAttribute('yChannelSelector', 'G');
    displace.setAttribute('scale', String(depth * 2));
    filter.append(map, displace);
    // A sharp rim: the bent backdrop under the light rim blur across the
    // bezel (the map's blue channel), over the backdrop under the full blur.
    // The blur is then part of the filter rather than the CSS chain.
    const sharpRim = rimBlur !== undefined && rimBlur !== blur;
    if (sharpRim) {
      displace.setAttribute('result', 'bent');
      const primitive = (name: string, attributes: Record<string, string>) => {
        const node = document.createElementNS(svgNamespace, name);
        for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value);
        filter.append(node);
      };
      primitive('feGaussianBlur', { in: 'bent', stdDeviation: String(rimBlur), edgeMode: 'duplicate', result: 'rim' });
      primitive('feGaussianBlur', { in: 'SourceGraphic', stdDeviation: String(blur), edgeMode: 'duplicate', result: 'middle' });
      primitive('feColorMatrix', { in: 'map', type: 'matrix', values: '0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 1 0 0', result: 'mask' });
      primitive('feComposite', { in: 'rim', in2: 'mask', operator: 'in', result: 'edge' });
      primitive('feComposite', { in: 'edge', in2: 'middle', operator: 'over' });
    }
    filterDefs().appendChild(filter);

    const apply = () => {
      const width = Math.round(element.offsetWidth);
      const height = Math.round(element.offsetHeight);
      if (!width || !height) return;
      const radius = Math.min(parseFloat(getComputedStyle(element).borderTopLeftRadius) || 0, width / 2, height / 2);
      for (const node of [filter, map]) {
        node.setAttribute('width', String(width));
        node.setAttribute('height', String(height));
      }
      map.setAttribute('href', displacementMap({ width, height, radius, bezel: Math.min(bezel, Math.max(radius, 4)) }));
      const value = `url(#${id})${sharpRim ? '' : ` blur(${blur}px)`} saturate(${saturate}%)${tone ? ` ${tone}` : ''}`;
      element.style.setProperty('-webkit-backdrop-filter', value);
      element.style.setProperty('backdrop-filter', value);
    };

    const observer = new ResizeObserver(apply);
    observer.observe(element);
    apply();
    return () => {
      observer.disconnect();
      filter.remove();
      element.style.removeProperty('-webkit-backdrop-filter');
      element.style.removeProperty('backdrop-filter');
    };
  }, [bezel, depth, blur, rimBlur, saturate, tone]);

  return ref;
}
