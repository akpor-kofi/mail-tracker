'use client';
import { useEffect } from 'react';
import { EditorContent, useEditor } from '@tiptap/react';
import StarterKit from '@tiptap/starter-kit';
import { Button } from '@/components/ui/button';
export function Editor({ value, onChange }: { value: string; onChange: (html: string) => void }) {
  const editor = useEditor({
    extensions: [StarterKit],
    content: value,
    immediatelyRender: false,
    editorProps: { attributes: { id: 'message-editor', 'aria-label': 'Message' } },
    onUpdate: ({ editor }) => onChange(editor.getHTML()),
  });
  useEffect(() => {
    if (editor && editor.getHTML() !== value) editor.commands.setContent(value);
  }, [editor, value]);
  return (
    <div className="editor">
      <div className="editor-toolbar">
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={() => editor?.chain().focus().toggleBold().run()}
          aria-label="Bold"
          aria-pressed={editor?.isActive('bold') ?? false}
        >
          <strong>B</strong>
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          onClick={() => editor?.chain().focus().toggleItalic().run()}
          aria-label="Italic"
          aria-pressed={editor?.isActive('italic') ?? false}
        >
          <em>I</em>
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => editor?.chain().focus().toggleBulletList().run()}
          aria-label="Bullet list"
          aria-pressed={editor?.isActive('bulletList') ?? false}
        >
          • List
        </Button>
      </div>
      <EditorContent editor={editor} />
    </div>
  );
}
