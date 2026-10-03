import ReactMarkdown, { type Components } from 'react-markdown';

// Assistant text is untrusted: react-markdown never renders raw HTML (no
// rehype-raw), and links open in a new tab without referrer/opener access.
const components: Components = {
  a: ({ href, children }) => {
    const safe = href && /^https?:\/\//i.test(href) ? href : undefined;
    return safe ? (
      <a href={safe} target="_blank" rel="noopener noreferrer nofollow">
        {children}
      </a>
    ) : (
      <span>{children}</span>
    );
  },
  img: () => null,
};

export function Markdown({ text }: { text: string }) {
  return (
    <div className="md">
      <ReactMarkdown components={components} skipHtml>
        {text}
      </ReactMarkdown>
    </div>
  );
}
