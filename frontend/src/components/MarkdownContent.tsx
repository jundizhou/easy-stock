import ReactMarkdown from 'react-markdown';
import remarkBreaks from 'remark-breaks';
import remarkGfm from 'remark-gfm';

type Props = {
	content: string;
	markdown?: boolean;
};

export function MessageContent({ content, markdown = false }: Props) {
	if (!markdown) {
		return <div className="ai-message-content ai-message-content-plain">{content}</div>;
	}

	return (
		<div className="ai-message-content ai-markdown">
			<ReactMarkdown
				remarkPlugins={[remarkGfm, remarkBreaks]}
				skipHtml
				components={{
					a: ({ children, href }) => (
						<a href={href} target="_blank" rel="noreferrer">{children}</a>
					),
				}}
			>
				{content}
			</ReactMarkdown>
		</div>
	);
}
