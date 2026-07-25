import type { ComponentPropsWithoutRef } from 'react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import clsx from 'clsx';

function safeMarkdownUrl(url: string) {
  return /^(https?:|mailto:|\/|#)/i.test(url) ? url : '';
}

function MarkdownLink({ href = '', children, title }: ComponentPropsWithoutRef<'a'>) {
  const external = /^(https?:|mailto:)/i.test(href);
  return (
    <a
      href={href}
      title={title}
      className="text-primary underline underline-offset-2 hover:text-primary/80"
      {...(external ? { target: '_blank', rel: 'noreferrer' } : {})}
    >
      {children}
    </a>
  );
}

const markdownComponents: Components = {
  h1: ({ children }) => <h1 className="text-lg font-semibold leading-7">{children}</h1>,
  h2: ({ children }) => <h2 className="text-base font-semibold leading-6">{children}</h2>,
  h3: ({ children }) => <h3 className="font-semibold leading-6">{children}</h3>,
  h4: ({ children }) => <h4 className="font-semibold leading-6">{children}</h4>,
  p: ({ children }) => <p className="whitespace-pre-wrap break-words">{children}</p>,
  strong: ({ children }) => <strong className="font-semibold">{children}</strong>,
  ul: ({ children }) => <ul className="list-disc space-y-1 pl-5">{children}</ul>,
  ol: ({ children }) => <ol className="list-decimal space-y-1 pl-5">{children}</ol>,
  li: ({ children }) => <li className="pl-1">{children}</li>,
  blockquote: ({ children }) => (
    <blockquote className="border-l-2 border-border pl-3 text-muted-foreground">{children}</blockquote>
  ),
  a: MarkdownLink,
  pre: ({ children }) => (
    <pre className="subtle-scrollbar overflow-x-auto rounded-md border border-border bg-secondary p-3 font-mono text-sm leading-6 [&>code]:bg-transparent [&>code]:p-0">
      {children}
    </pre>
  ),
  code: ({ className, children }) => (
    <code className={clsx('rounded bg-secondary px-1 py-0.5 font-mono text-[0.92em]', className)}>{children}</code>
  ),
  table: ({ children }) => (
    <div className="subtle-scrollbar overflow-x-auto rounded-md border border-border">
      <table className="min-w-full border-collapse text-left text-sm">{children}</table>
    </div>
  ),
  th: ({ children }) => <th className="border-b border-border bg-secondary px-3 py-2 font-semibold">{children}</th>,
  td: ({ children }) => <td className="border-t border-border px-3 py-2 align-top">{children}</td>,
  hr: () => <hr className="border-border" />,
  img: () => null,
};

export function MarkdownContent({ value, className }: { value: string; className?: string }) {
  return (
    <div className={clsx('space-y-3 whitespace-normal break-words', className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={markdownComponents}
        skipHtml
        urlTransform={safeMarkdownUrl}
      >
        {value}
      </ReactMarkdown>
    </div>
  );
}
