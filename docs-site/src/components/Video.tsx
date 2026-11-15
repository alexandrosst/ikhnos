import useBaseUrl from '@docusaurus/useBaseUrl';
import {useEffect, useRef, useState} from 'react';
import type {ReactNode} from 'react';

type Props = {
  /** The file under static/video without the extension: "tour" is tour.mp4, with tour.jpg as its first picture. */
  src: string;
  /** What the video shows, for someone who cannot see it. */
  label: string;
  caption?: ReactNode;
};

const SPEEDS = [0.5, 1, 1.5, 2];

const clock = (s: number) => {
  const t = Math.max(0, Math.floor(Number.isFinite(s) ? s : 0));
  return `${Math.floor(t / 60)}:${String(t % 60).padStart(2, '0')}`;
};

/**
 * A short screen recording of the real product, in the frame of a screenshot. It has no sound: the captions in it say what is happening.
 * The controls sit in a bar under the picture, so nothing ever covers the frame, in the page or in full screen.
 */
export default function Video({src, label, caption}: Props) {
  const mp4 = useBaseUrl(`/video/${src}.mp4`);
  const poster = useBaseUrl(`/video/${src}.jpg`);
  const box = useRef<HTMLElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  const [playing, setPlaying] = useState(false);
  const [time, setTime] = useState(0);
  const [length, setLength] = useState(0);
  const [speed, setSpeed] = useState(1);
  const [full, setFull] = useState(false);

  useEffect(() => {
    const on = () => setFull(document.fullscreenElement === box.current);
    document.addEventListener('fullscreenchange', on);
    return () => document.removeEventListener('fullscreenchange', on);
  }, []);

  const toggle = () => {
    const v = video.current;
    if (!v) return;
    if (v.paused) void v.play();
    else v.pause();
  };
  const seek = (to: number) => {
    const v = video.current;
    if (v) v.currentTime = to;
    setTime(to);
  };
  const faster = (to: number) => {
    setSpeed(to);
    if (video.current) video.current.playbackRate = to;
  };
  const screen = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void box.current?.requestFullscreen?.();
  };

  return (
    <figure ref={box} className={`ikh-shot ikh-video${full ? ' ikh-video--full' : ''}`}>
      <video
        ref={video}
        preload="metadata"
        playsInline
        poster={poster}
        aria-label={label}
        onClick={toggle}
        onPlay={() => setPlaying(true)}
        onPause={() => setPlaying(false)}
        onEnded={() => setPlaying(false)}
        onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
        onLoadedMetadata={(e) => setLength(e.currentTarget.duration)}
      >
        <source src={mp4} type="video/mp4" />
      </video>
      <div className="ikh-video__bar" role="group" aria-label="Video controls">
        <button type="button" onClick={toggle} aria-label={playing ? 'Pause' : 'Play'} title={playing ? 'Pause' : 'Play'}>
          {playing ? (
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 3h3v10H4zM9 3h3v10H9z" /></svg>
          ) : (
            <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 2.5v11l9-5.5z" /></svg>
          )}
        </button>
        <span className="ikh-video__time">{clock(time)}</span>
        <input
          type="range"
          min={0}
          max={length || 0}
          step={0.1}
          value={Math.min(time, length || 0)}
          onChange={(e) => seek(Number(e.target.value))}
          aria-label="Position"
          aria-valuetext={`${clock(time)} of ${clock(length)}`}
        />
        <span className="ikh-video__time">{clock(length)}</span>
        <select value={speed} onChange={(e) => faster(Number(e.target.value))} aria-label="Speed">
          {SPEEDS.map((x) => (
            <option key={x} value={x}>{x}×</option>
          ))}
        </select>
        <button type="button" onClick={screen} aria-label={full ? 'Leave full screen' : 'Full screen'} title={full ? 'Leave full screen' : 'Full screen'}>
          <svg viewBox="0 0 16 16" aria-hidden="true">
            {full ? (
              <path d="M6 2v4H2M10 2v4h4M6 14v-4H2M10 14v-4h4" fill="none" stroke="currentColor" strokeWidth="1.6" />
            ) : (
              <path d="M2 6V2h4M14 6V2h-4M2 10v4h4M14 10v4h-4" fill="none" stroke="currentColor" strokeWidth="1.6" />
            )}
          </svg>
        </button>
      </div>
      {caption && <figcaption>{caption}</figcaption>}
    </figure>
  );
}
