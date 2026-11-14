import useBaseUrl from '@docusaurus/useBaseUrl';
import type {ReactNode} from 'react';

type Props = {
  /** The file under static/video without the extension: "tour" is tour.mp4, with tour.jpg as its first picture. */
  src: string;
  /** What the video shows, for someone who cannot see it. */
  label: string;
  caption?: ReactNode;
};

/** A short screen recording of the real product, in the frame of a screenshot. It has no sound: the captions in it say what is happening. */
export default function Video({src, label, caption}: Props) {
  const mp4 = useBaseUrl(`/video/${src}.mp4`);
  const poster = useBaseUrl(`/video/${src}.jpg`);
  return (
    <figure className="ikh-shot ikh-video">
      <video controls preload="none" playsInline poster={poster} aria-label={label}>
        <source src={mp4} type="video/mp4" />
      </video>
      {caption && <figcaption>{caption}</figcaption>}
    </figure>
  );
}
