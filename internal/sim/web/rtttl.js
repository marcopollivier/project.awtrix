// Minimal RTTTL player (the format the clock's buzzer uses), square wave like a piezo.
const NOTES = { c: 0, 'c#': 1, d: 2, 'd#': 3, e: 4, f: 5, 'f#': 6, g: 7, 'g#': 8, a: 9, 'a#': 10, b: 11, h: 11 };
let ac;

export function playRTTTL(song) {
  const [, defs = '', body = ''] = song.split(':');
  const def = Object.fromEntries(defs.split(',').map(p => p.trim().split('=')).filter(p => p.length === 2));
  const d = +def.d || 4, o = +def.o || 6, bpm = +def.b || 63;
  const whole = (60 / bpm) * 4;

  ac ??= new AudioContext();
  let t = ac.currentTime + 0.05;
  for (const raw of body.split(',')) {
    const m = raw.trim().toLowerCase().match(/^(\d+)?([a-hp]#?)(\.?)(\d)?(\.?)$/);
    if (!m) continue;
    let dur = whole / (+m[1] || d);
    if (m[3] || m[5]) dur *= 1.5;
    if (m[2] !== 'p') {
      const octave = +m[4] || o;
      const midi = 12 * (octave + 1) + NOTES[m[2]];
      const osc = ac.createOscillator();
      const gain = ac.createGain();
      osc.type = 'square';
      osc.frequency.value = 440 * 2 ** ((midi - 69) / 12);
      gain.gain.value = 0.05;
      osc.connect(gain).connect(ac.destination);
      osc.start(t);
      osc.stop(t + dur * 0.9);
    }
    t += dur;
  }
}
