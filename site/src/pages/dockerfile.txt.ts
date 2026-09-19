import dockerfile from '../../../Dockerfile?raw';

export const prerender = true;
export function GET() {
  return new Response(dockerfile, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
}
