import { useEffect, useRef } from "react"

const PUPIL_DISTANCE = 10
const BLINK_DURATION_MS = 140

const OPEN_TOP_PATH = "M10,50 Q50,15 90,50"
const OPEN_BOTTOM_PATH = "M10,50 Q50,85 90,50"
const OPEN_CLIP_PATH = "M10,50 Q50,15 90,50 Q50,85 10,50"
const CLOSED_LID_PATH = "M10,50 Q50,50 90,50"
const CLOSED_CLIP_PATH = "M10,50 Q50,50 90,50 Q50,50 10,50"

type LogoProps = {
	className?: string
	interactive?: boolean
}

export function Logo({ className, interactive = false }: LogoProps) {
	const svgRef = useRef<SVGSVGElement | null>(null)
	const pupilRef = useRef<SVGCircleElement | null>(null)
	const topLidRef = useRef<SVGPathElement | null>(null)
	const bottomLidRef = useRef<SVGPathElement | null>(null)
	const clipRef = useRef<SVGPathElement | null>(null)
	const blinkTimeoutRef = useRef<number | null>(null)
	const animationFrameRef = useRef<number | null>(null)

	useEffect(() => {
		if (!interactive) return

		const updatePupilPosition = (event: MouseEvent) => {
			const svg = svgRef.current
			const pupil = pupilRef.current
			if (!svg || !pupil) return

			const rect = svg.getBoundingClientRect()
			const centerX = rect.left + rect.width / 2
			const centerY = rect.top + rect.height / 2
			const angle = Math.atan2(event.clientY - centerY, event.clientX - centerX)

			const x = Math.cos(angle) * PUPIL_DISTANCE
			const y = Math.sin(angle) * PUPIL_DISTANCE

			if (animationFrameRef.current !== null) {
				window.cancelAnimationFrame(animationFrameRef.current)
			}

			animationFrameRef.current = window.requestAnimationFrame(() => {
				pupil.style.transform = `translate(${x}px, ${y}px)`
				animationFrameRef.current = null
			})
		}

		const triggerBlink = () => {
			const svg = svgRef.current
			const topLid = topLidRef.current
			const bottomLid = bottomLidRef.current
			const clip = clipRef.current
			if (!svg || !topLid || !bottomLid || !clip || svg.dataset.blinking === "true") return

			svg.dataset.blinking = "true"
			topLid.setAttribute("d", CLOSED_LID_PATH)
			bottomLid.setAttribute("d", CLOSED_LID_PATH)
			clip.setAttribute("d", CLOSED_CLIP_PATH)

			if (blinkTimeoutRef.current !== null) window.clearTimeout(blinkTimeoutRef.current)
			blinkTimeoutRef.current = window.setTimeout(() => {
				topLid.setAttribute("d", OPEN_TOP_PATH)
				bottomLid.setAttribute("d", OPEN_BOTTOM_PATH)
				clip.setAttribute("d", OPEN_CLIP_PATH)
				delete svg.dataset.blinking
				blinkTimeoutRef.current = null
			}, BLINK_DURATION_MS)
		}

		window.addEventListener("mousemove", updatePupilPosition)
		window.addEventListener("mousedown", triggerBlink)

		return () => {
			window.removeEventListener("mousemove", updatePupilPosition)
			window.removeEventListener("mousedown", triggerBlink)
			if (blinkTimeoutRef.current !== null) window.clearTimeout(blinkTimeoutRef.current)
			if (animationFrameRef.current !== null) window.cancelAnimationFrame(animationFrameRef.current)
		}
	}, [interactive])

	if (!interactive) {
		return (
			<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" className={className} aria-hidden="true">
				<rect x="12" y="12" width="10" height="40" rx="5" fill="currentColor" />
				<rect x="42" y="12" width="10" height="40" rx="5" fill="currentColor" />
				<path fill="currentColor" d="M24 18h8l8 14.4V46h-8L24 31.6z" opacity="0.9" />
			</svg>
		)
	}

	return (
		<svg ref={svgRef} xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100" className={className} aria-hidden="true">
			<defs>
				<clipPath id="logo-eye-clip">
					<path ref={clipRef} d={OPEN_CLIP_PATH} />
				</clipPath>
			</defs>
			<circle ref={pupilRef} className="logo-eye-pupil" cx="50" cy="50" r="14" clipPath="url(#logo-eye-clip)" />
			<path ref={topLidRef} className="logo-eye-lid" d={OPEN_TOP_PATH} />
			<path ref={bottomLidRef} className="logo-eye-lid" d={OPEN_BOTTOM_PATH} />
		</svg>
	)
}
