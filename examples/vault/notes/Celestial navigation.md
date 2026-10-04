---
title: Celestial navigation
aliases: [Astro navigation, Sight reduction notes]
tags: [navigation, learning, longform]
started: 2026-08-02
---
# Celestial navigation

Finding where you are from the angle between a star and the horizon, a good
watch and a book of tables. I am learning it for the pleasure of it, with a
cheap plastic sextant, on a lake shore where the horizon is honest. These
notes are my own summary; they are not a substitute for a proper course.

> [!abstract] The whole method in four sentences
> Every star is directly overhead somewhere on Earth at any moment. Measure
> how far the star is from overhead *for you*, and you know how far you are
> from that spot: you are somewhere on a circle around it. Two stars give
> two circles, and you are where they cross. Everything else is bookkeeping.

## Contents

1. [[#Vocabulary]]
2. [[#The circle of position]]
3. [[#Instruments]]
4. [[#Time]]
5. [[#The noon sight]]
6. [[#Polaris and latitude]]
7. [[#Sight reduction]]
8. [[#Plotting]]
9. [[#Stars worth knowing]]
10. [[#Errors and judgement]]
11. [[#Practice log]]

## Vocabulary

The terms come in pairs: one for the sky, one for the Earth. Most confusion
I had in the first week came from mixing them.

- **Altitude** — the angle of a body above the horizon, 0° to 90°.
  - *Hs* is the sextant reading, *Ho* the observed altitude after
    corrections, *Hc* the altitude computed for an assumed position.
- **Azimuth** — the true bearing of the body, measured clockwise from
  north. Written *Zn*.
- **Geographic position (GP)** — the point on Earth directly beneath the
  body. It moves west at about 15° of longitude per hour.
- **Declination** — the latitude of the GP, north or south.
- **Greenwich hour angle (GHA)** — the longitude of the GP, measured
  westward from Greenwich, 0° to 360°.
- **Local hour angle (LHA)** — the same angle measured from *your*
  meridian: `LHA = GHA + east longitude` or `GHA − west longitude`.
- **Zenith distance** — 90° minus the altitude. It is the radius of the
  circle of position, in degrees of arc.
- **Intercept** — the difference between Ho and Hc, in minutes of arc,
  which are nautical miles.

> [!tip] The one conversion to remember
> One minute of arc on a great circle is one nautical mile. So one degree
> is sixty miles, and every angle is secretly a distance.

## The circle of position

Picture a lighthouse with a known height. If you measure the angle from
its base to its top, you know your distance from it, but not your
direction: you are somewhere on a circle around the lighthouse.

A star works the same way, except that the "lighthouse" is the star's GP
and its height is infinite. The angle you measure, the altitude, tells you
the zenith distance, and so the radius of the circle:

$$
r = (90^\circ - H_o) \times 60 \ \text{nautical miles}
$$

For a star at 40° altitude that radius is 3,000 miles — far too large to
draw on any chart. That is why nobody draws the circle. Instead we pick an
assumed position close to where we think we are and compute what the
altitude *would* be there. The difference tells us how far to move the
line towards or away from the GP.

- If **Ho is greater than Hc**, we are closer to the GP than assumed:
  move *towards* it.
- If **Ho is less than Hc**, we are farther away: move *away*.

Mnemonic from the course: *HoMoTo* — Ho More, Towards.

Near the assumed position the huge circle is indistinguishable from a
straight line at right angles to the azimuth. That straight piece is the
**line of position** (LOP), and it is what goes on the chart.

## Instruments

### The sextant

A sextant measures the angle between two things by bringing the image of
one down onto the other with two mirrors. The arc is a sixth of a circle,
but thanks to the double reflection it reads up to 120°.

Parts, from the top:

- the **index arm**, which pivots and carries the index mirror;
- the **horizon mirror**, half silvered, half clear;
- **shades** for the Sun, in front of both mirrors;
- the **micrometer drum**, which reads minutes and tenths;
- the **telescope**, which I mostly leave off on the lake.

To take a sight: set the arm near the expected altitude, find the body,
bring it down to the horizon, then rock the sextant gently from side to
side. The body swings in an arc; at the bottom of the arc it should just
kiss the horizon. Call "mark" and note the time to the second.

### Corrections from Hs to Ho

The reading is never the answer. Each correction is small, but together
they easily reach a quarter of a degree, which is fifteen miles.

| Correction | Cause | Typical size | Sign |
|:--|:--|--:|:--:|
| Index error | the mirrors are not parallel at zero | 0′–5′ | ± |
| Dip | your eye is above sea level | 1.8′ × √height in m | − |
| Refraction | air bends light near the horizon | 34′ at 0°, 1′ at 45° | − |
| Semidiameter | we measure the Sun's edge, not its centre | about 16′ | ± |
| Parallax | we are on the surface, not at the centre | tiny except the Moon | + |

The order matters less than doing all of them every time. My checklist:

1. Read the index error before *and* after the session; use the mean.
2. Apply dip for the height of my eye above the water (about 2 m on the
   jetty, so about −2.5′).
3. Apply refraction from the table for the apparent altitude.
4. For the Sun, add the semidiameter for a lower-limb sight.

### Watch and almanac

- A **watch** that I check against a time signal every session. I note its
  error rather than setting it, so I can see the drift.
- The **almanac** gives the GHA and declination of the Sun, Moon, planets
  and the first point of Aries for every hour of the year.
- **Sight reduction tables** (or the formula below) turn assumed position,
  declination and LHA into Hc and Zn.

## Time

Time is longitude. The Earth turns 360° in 24 hours, so:

$$
15^\circ \text{ per hour} = 15' \text{ per minute} = 1' \text{ every 4 seconds}
$$

At the equator one minute of arc of longitude is one nautical mile, so a
watch that is four seconds wrong puts you a mile east or west of the truth.
Away from the equator the miles shrink with the cosine of the latitude, but
the habit stays: *know your clock error*.

| Clock error | Longitude error | Distance at the equator | At 50° N |
|--:|--:|--:|--:|
| 1 s | 0.25′ | 0.25 nm | 0.16 nm |
| 4 s | 1′ | 1 nm | 0.64 nm |
| 30 s | 7.5′ | 7.5 nm | 4.8 nm |
| 2 min | 30′ | 30 nm | 19.3 nm |

This is the problem the marine chronometer solved in the eighteenth
century. Before it, sailors ran down a known latitude and then turned east
or west, because latitude was easy and longitude was not.

## The noon sight

The simplest sight in the book, and the one I started with. At local noon
the Sun crosses your meridian: it is due south (or north) and at its
highest. No azimuth, no LHA, no tables beyond the declination.

1. Start observing about fifteen minutes before the expected noon.
2. Keep bringing the Sun's lower limb down to the horizon as it rises.
3. When it stops rising — it seems to hang for a minute or two — that
   reading is the maximum. Note it; the exact time matters less.
4. Correct Hs to Ho.
5. Look up the declination for the date and approximate time.
6. Compute the latitude.

With the Sun south of you and declination signed (north positive):

$$
\text{Lat} = (90^\circ - H_o) + \text{Dec}
$$

### Worked example

From my log on 3 October, on the jetty:

- Hs, lower limb: 51° 58.4′
- Index error: −0.6′ (reads high)
- Dip for 2 m: −2.5′
- Refraction and semidiameter, one combined table: +15.1′
- **Ho: 52° 10.4′**
- Declination: S 4° 12.0′, so −4° 12.0′

Zenith distance is 90° − 52° 10.4′ = 37° 49.6′, and

$$
\text{Lat} = 37^\circ 49.6' + (-4^\circ 12.0') = 33^\circ 37.6' \ \text{N}
$$

> [!warning] Sign of the declination
> In October the Sun is south of the equator. The first time I did this I
> added the declination as if it were north and put myself eight degrees
> too far north — about 500 miles, in a lake.

The combined table is where I went wrong that day: I used it *and* added
the 16′ semidiameter by hand, counting it twice. The figures shown are
after fixing it. Lesson: one table, used once.

## Polaris and latitude

In the northern hemisphere Polaris sits less than a degree from the
celestial pole, so its altitude is very nearly your latitude. The almanac
gives three small corrections (a0, a1, a2) that account for its offset and
the date; together they rarely exceed a degree.

$$
\text{Lat} = H_o - 1^\circ + a_0 + a_1 + a_2
$$

Polaris is faint (magnitude about 2), and it has to be shot at twilight,
when both the star and the horizon are visible. That window is about
twenty minutes long, and it is the best time of day.

## Sight reduction

For anything other than noon or Polaris, we compute the altitude and
azimuth the body *would* have from an assumed position, and compare.

Inputs: assumed latitude **L**, declination **d** and local hour angle
**LHA**. The computed altitude follows from the spherical law of cosines:

$$
\sin H_c = \sin L \sin d + \cos L \cos d \cos \text{LHA}
$$

and the azimuth angle **Z** from

$$
\cos Z = \frac{\sin d - \sin L \sin H_c}{\cos L \cos H_c}
$$

with `Zn = Z` when LHA is greater than 180° (the body is east of you) and
`Zn = 360° − Z` otherwise.

I check my table work with a few lines of Python:

```python
from math import radians, degrees, sin, cos, asin, acos

def reduce_sight(lat, dec, lha):
    """Computed altitude Hc and true azimuth Zn, all in degrees.

    lat and dec are signed (north positive); lha is 0-360.
    """
    L, d, t = radians(lat), radians(dec), radians(lha)
    hc = asin(sin(L) * sin(d) + cos(L) * cos(d) * cos(t))
    z = degrees(acos((sin(d) - sin(L) * sin(hc)) / (cos(L) * cos(hc))))
    zn = z if lha > 180 else 360 - z
    return degrees(hc), zn

def intercept(ho, hc):
    """Intercept in nautical miles: positive means towards the body."""
    return (ho - hc) * 60

if __name__ == "__main__":
    hc, zn = reduce_sight(lat=34.0, dec=-4.2, lha=20.0)
    print(f"Hc {hc:6.2f}°  Zn {zn:6.1f}°")
```

The tables exist because the formula was painful by hand; they also avoid
the trap of `acos` near 0° and 180°, where rounding makes the azimuth
jump. For a learner the formula is clearer and the tables are faster.

## Plotting

On a plotting sheet, with the assumed position in the middle:

1. Draw the azimuth line through the assumed position, at Zn.
2. Measure the intercept along it — towards the body if Ho > Hc, away if
   not.
3. Draw the line of position at right angles to the azimuth through that
   point.
4. Repeat for the second and third bodies.
5. Where the lines cross is the fix. Three lines rarely meet in a point;
   they make a small triangle, the *cocked hat*.

> [!example] A good set of three
> Choose bodies roughly 120° apart in azimuth. Then a constant error (a
> bad dip, a wrong clock) moves all three lines the same way and the
> triangle stays small and honest, instead of stretching into a sliver.

### Running fix

With only the Sun available, take a morning sight, sail (or, on the lake,
walk) a known course and distance, then take an afternoon sight. Advance
the morning line by the run and cross it with the afternoon line. The
accuracy depends on the run, which on water is never as good as you hope.

## Stars worth knowing

Fifty-seven stars are listed in the almanac for navigation. These are the
bright ones I can find without a chart, with approximate magnitudes[^1]
(lower is brighter).

| Star | Constellation | Magnitude | When I see it |
|:--|:--|--:|:--|
| Sirius | Canis Major | −1.5 | winter evenings |
| Arcturus | Boötes | 0.0 | spring and summer |
| Vega | Lyra | 0.0 | summer, overhead |
| Capella | Auriga | 0.1 | autumn and winter |
| Rigel | Orion | 0.1 | winter |
| Procyon | Canis Minor | 0.4 | winter |
| Altair | Aquila | 0.8 | summer and autumn |
| Aldebaran | Taurus | 0.9 | winter |
| Spica | Virgo | 1.0 | spring |
| Antares | Scorpius | 1.0 | summer, low in the south |
| Pollux | Gemini | 1.1 | winter and spring |
| Fomalhaut | Piscis Austrinus | 1.2 | autumn, very low |
| Deneb | Cygnus | 1.3 | summer and autumn |
| Regulus | Leo | 1.4 | spring |

The planets are brighter than all of these except Sirius, and they wander,
which makes them easy to mistake for a star the first few times. Venus at
dusk is the classic.

### Finding them

- **Arc to Arcturus, speed on to Spica**: follow the curve of the Plough's
  handle.
- The **Summer Triangle** is Vega, Deneb and Altair; Vega is the bright
  corner nearly overhead in August.
- **Orion's belt** points down-left to Sirius and up-right to Aldebaran.
- The two end stars of the Plough's bowl point at Polaris.

## Errors and judgement

The method is exact; the observer is not. My errors, roughly in order of
how often they happen:

1. **Time** — reading the watch a few seconds late after calling "mark".
   A helper with a stopwatch fixes it.
2. **Horizon** — haze makes the horizon look closer and lower; the reading
   comes out a few minutes high.
3. **Arithmetic** — degrees and minutes do not add like decimals. 37° 49.6′
   plus 0° 12.0′ is 38° 01.6′, not 37° 61.6′.
4. **Tables** — reading the wrong column, or the wrong day.
5. **The instrument** — a plastic sextant changes its index error as it
   warms in the Sun. Measure it again after ten minutes.

> [!note] What the cocked hat says
> A small triangle does not prove a good fix; three bodies close together
> in azimuth can make a small, confidently wrong triangle. A large triangle
> from well-spread bodies is honest about the uncertainty. Trust the honest
> one.

A navigator's real skill, as far as I can tell, is deciding how much to
believe each line, and keeping a dead-reckoning position going in parallel
so that a wild fix is noticed.

## History, briefly

Long before the sextant, the astrolabe let observers measure the altitude
of the Sun and stars and read off the time; my notes on it are in Arabic,
in [[الأسطرلاب]]. Sailors later carried a stripped-down version, the
mariner's astrolabe, then the quadrant, the cross-staff and the backstaff,
and finally the octant and sextant in the eighteenth century. The
chronometer arrived at about the same time and turned longitude from an
art into arithmetic.

The ideas also show up at work, oddly: a fix is just three independent
measurements whose disagreement tells you how much to trust them, which is
also how I think about the shadow consumers in [[Lantern migration]].

## Practice log

### August

- **2 Aug** — first session. Could not find the Sun in the mirror for ten
  minutes. Index error −2.0′.
- **9 Aug** — five noon sights in a row; spread of 4′ between them. Mean
  latitude 9′ north of the map.
- **16 Aug** — learned to rock the sextant. Spread down to 2′.
- **23 Aug** — first Polaris at dusk. Lost the horizon before I found the
  star; tried again the next evening and got it.
- **30 Aug** — three-star fix from Vega, Arcturus and Altair. Cocked hat
  about 6 miles across; good for a first try.

### September

- **6 Sep** — worked the same three stars with tables and with the Python
  script; they agree to 0.2′.
- **13 Sep** — hazy horizon, results 5′ high, as the book warned.
- **20 Sep** — running fix with the Sun, morning and afternoon, walking
  2.4 km along the shore between them. Fix within 1.5 miles.
- **27 Sep** — index error drifted from −0.6′ to −1.4′ in twenty minutes
  of sunshine. Started re-checking mid-session.

### October

- **3 Oct** — the noon sight in the worked example above; the semidiameter
  mistake. Corrected latitude within 1′ of the map.

### Next

- [ ] Shoot the Moon (upper and lower limb) and work it through 📅 2026-10-11
- [ ] Learn the planets' almanac pages
- [ ] Three-star fix with bodies 120° apart and a helper on the stopwatch 📅 2026-10-17
- [ ] Try a sun-run-sun on the dark-sky trip in November
- [x] Buy a proper star chart instead of the phone app

## Further reading

- The almanac's own explanation pages, which are better than most books.
- Any good introductory course on coastal navigation first: celestial
  methods build on dead reckoning and chart work.
- [[The Quiet Machine]], not about the sea at all, but its chapter on
  feedback loops reads well next to the section on errors.

[^1]: The magnitudes in the star table are rounded and some of those stars
    vary a little in brightness.

#navigation #longform
