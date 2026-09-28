import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { cn } from '../lib/cn';

// Rendered markdown viewer (GFM: tables, task lists, strikethrough).
// Styling comes from the `.md-body` block in index.css (dark + light aware).
// react-markdown never renders raw HTML, so no sanitizer is needed.
export function MarkdownView({ content, className }: { content: string; className?: string }) {
  if (!content.trim()) {
    return <div className={cn('p-3 text-[12px] text-zinc-600', className)}>Nothing to preview yet.</div>;
  }
  return (
    <div className={cn('md-body', className)}>
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </div>
  );
}
