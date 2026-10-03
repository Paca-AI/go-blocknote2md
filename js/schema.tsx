import {
	BlockNoteSchema,
	defaultBlockSpecs,
	defaultInlineContentSpecs,
} from "@blocknote/core";
import {
	createReactBlockSpec,
	createReactInlineContentSpec,
} from "@blocknote/react";

// The three extensions Paca (https://github.com/Paca-AI/paca) registers on top
// of BlockNote's defaults. Only what affects Markdown export is reproduced:
// toExternalHTML for the blocks, and the render output (an icon plus the name)
// for the inline content, which has no toExternalHTML of its own. The icons are
// plain <svg> elements here; the exporter ignores their content.

const mermaid = createReactBlockSpec(
	{
		type: "mermaid",
		propSchema: { code: { default: "" } },
		content: "none",
	},
	{
		render: () => <div />,
		toExternalHTML: ({ block }) => (
			<pre>
				<code className="language-mermaid">
					{(block.props as { code: string }).code}
				</code>
			</pre>
		),
	},
);

const annotationCard = createReactBlockSpec(
	{
		type: "annotationCard",
		propSchema: {
			id: { default: "" },
			projectId: { default: "" },
			environmentId: { default: "" },
			portForwardId: { default: "" },
		},
		content: "none",
	},
	{
		render: () => <div />,
		toExternalHTML: ({ block }) => {
			const { id, projectId, environmentId, portForwardId } = block.props as {
				id: string;
				projectId: string;
				environmentId: string;
				portForwardId: string;
			};
			const url = `${window.location.origin}/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}/comments/${id}`;
			return <a href={url}>Comment</a>;
		},
	},
);

const teamMention = createReactInlineContentSpec(
	{
		type: "teamMention",
		propSchema: {
			id: { default: "" },
			name: { default: "Unknown" },
			avatar: { default: undefined, type: "string" as const },
		},
		content: "none",
	},
	{
		render: (props) => (
			<span data-mention-type="team">
				<svg aria-hidden="true" />
				{props.inlineContent.props.name}
			</span>
		),
	},
);

const taskReference = createReactInlineContentSpec(
	{
		type: "taskReference",
		propSchema: {
			id: { default: "" },
			title: { default: "Unknown" },
			status: { default: "open" },
		},
		content: "none",
	},
	{
		render: (props) => (
			<span data-mention-type="task">
				<svg aria-hidden="true" />
				{props.inlineContent.props.title}
			</span>
		),
	},
);

const docReference = createReactInlineContentSpec(
	{
		type: "docReference",
		propSchema: {
			id: { default: "" },
			title: { default: "Unknown" },
		},
		content: "none",
	},
	{
		render: (props) => (
			<span data-mention-type="doc">
				<svg aria-hidden="true" />
				{props.inlineContent.props.title}
			</span>
		),
	},
);

export const schema = BlockNoteSchema.create({
	blockSpecs: {
		...defaultBlockSpecs,
		mermaid: mermaid(),
		annotationCard: annotationCard(),
	},
	inlineContentSpecs: {
		...defaultInlineContentSpecs,
		teamMention,
		taskReference,
		docReference,
	},
});
