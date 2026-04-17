import { t } from "@lingui/core/macro"
import { useStore } from "@nanostores/react"
import type { AuthMethodsList } from "pocketbase"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { UserAuthForm } from "@/components/login/auth-form"
import { pb } from "@/lib/api"
import { cn } from "@/lib/utils"
import { ModeToggle } from "../mode-toggle"
import { $router } from "../router"
import ForgotPassword from "./forgot-pass-form"
import { OtpRequestForm } from "./otp-forms"

const AUTH_CARD_SHAKE_MS = 400
const AUTH_FLASH_MS = 220
const AUTH_PUPIL_DISTANCE = 14

const CLOSED_TOP_PATH = "M10,50 Q50,50 90,50"
const CLOSED_BOTTOM_PATH = "M10,50 Q50,50 90,50"
const CLOSED_CLIP_PATH = "M10,50 Q50,50 90,50 Q50,50 10,50"
const OPEN_TOP_PATH = "M10,50 Q50,10 90,50"
const OPEN_BOTTOM_PATH = "M10,50 Q50,90 90,50"
const OPEN_CLIP_PATH = "M10,50 Q50,10 90,50 Q50,90 10,50"

function AuthEye({ isOpen, onOpen }: { isOpen: boolean; onOpen: () => void }) {
	const triggerRef = useRef<HTMLButtonElement | null>(null)
	const pupilRef = useRef<SVGCircleElement | null>(null)
	const animationFrameRef = useRef<number | null>(null)

	useEffect(() => {
		const pupil = pupilRef.current
		if (!pupil) return

		if (!isOpen) {
			pupil.style.transform = "translate(0px, 0px)"
			return
		}

		const handlePointerMove = (event: MouseEvent) => {
			const trigger = triggerRef.current
			const pupil = pupilRef.current
			if (!trigger || !pupil) return

			const rect = trigger.getBoundingClientRect()
			const centerX = rect.left + rect.width / 2
			const centerY = rect.top + rect.height / 2
			const angle = Math.atan2(event.clientY - centerY, event.clientX - centerX)

			const x = Math.cos(angle) * AUTH_PUPIL_DISTANCE
			const y = Math.sin(angle) * AUTH_PUPIL_DISTANCE

			if (animationFrameRef.current !== null) {
				window.cancelAnimationFrame(animationFrameRef.current)
			}

			animationFrameRef.current = window.requestAnimationFrame(() => {
				pupil.style.transform = `translate(${x}px, ${y}px)`
				animationFrameRef.current = null
			})
		}

		window.addEventListener("mousemove", handlePointerMove)

		return () => {
			window.removeEventListener("mousemove", handlePointerMove)
			if (animationFrameRef.current !== null) {
				window.cancelAnimationFrame(animationFrameRef.current)
			}
		}
	}, [isOpen])

	return (
		<button
			ref={triggerRef}
			type="button"
			className={cn("auth-eye-trigger", isOpen && "is-open")}
			onClick={onOpen}
			aria-label="Open Vigil eye"
		>
			<div className="auth-psycho-bg" />
			<svg className="auth-eye-svg" viewBox="0 0 100 100" aria-hidden="true">
				<defs>
					<clipPath id="auth-eye-clip">
						<path d={isOpen ? OPEN_CLIP_PATH : CLOSED_CLIP_PATH} />
					</clipPath>
				</defs>

				<g className="auth-eye-contents" clipPath="url(#auth-eye-clip)">
					<rect className="auth-eye-ball" x="0" y="0" width="100" height="100" />
					<circle ref={pupilRef} className="auth-eye-pupil" cx="50" cy="50" r="12" />
				</g>

				<path className="auth-eye-lid" d={isOpen ? OPEN_TOP_PATH : CLOSED_TOP_PATH} />
				<path className="auth-eye-lid" d={isOpen ? OPEN_BOTTOM_PATH : CLOSED_BOTTOM_PATH} />

				<g className="auth-eye-lashes">
					<line className="auth-eye-lash" x1="30" y1="50" x2="25" y2="60" />
					<line className="auth-eye-lash" x1="50" y1="50" x2="50" y2="65" />
					<line className="auth-eye-lash" x1="70" y1="50" x2="75" y2="60" />
				</g>
			</svg>
		</button>
	)
}

export default function () {
	const page = useStore($router)
	const [isFirstRun, setFirstRun] = useState(false)
	const [authMethods, setAuthMethods] = useState<AuthMethodsList>()
	const [isEyeOpen, setIsEyeOpen] = useState(false)
	const [isCardShaking, setIsCardShaking] = useState(false)
	const [flashMode, setFlashMode] = useState<"idle" | "accent" | "success">("idle")
	const shakeTimeoutRef = useRef<number | null>(null)
	const flashTimeoutRef = useRef<number | null>(null)
	const displayName = globalThis.APP.DISPLAY_NAME || "App"

	const openEye = useCallback(() => {
		setIsEyeOpen(true)
	}, [])

	const triggerShake = useCallback(() => {
		setIsCardShaking(true)
		if (shakeTimeoutRef.current !== null) window.clearTimeout(shakeTimeoutRef.current)
		shakeTimeoutRef.current = window.setTimeout(() => {
			setIsCardShaking(false)
			shakeTimeoutRef.current = null
		}, AUTH_CARD_SHAKE_MS)
	}, [])

	const triggerFlash = useCallback((mode: "accent" | "success") => {
		setFlashMode(mode)
		if (flashTimeoutRef.current !== null) window.clearTimeout(flashTimeoutRef.current)
		flashTimeoutRef.current = window.setTimeout(() => {
			setFlashMode("idle")
			flashTimeoutRef.current = null
		}, AUTH_FLASH_MS)
	}, [])

	const handleSubmitStart = useCallback(() => {
		triggerShake()
		triggerFlash("accent")
	}, [triggerFlash, triggerShake])

	const handleSubmitSuccess = useCallback(() => {
		triggerFlash("success")
	}, [triggerFlash])

	const handleSubmitError = useCallback(() => {
		triggerShake()
	}, [triggerShake])

	useEffect(() => {
		document.title = `${t`Login`} / ${displayName}`

		pb.send("/api/app/first-run", {}).then(({ firstRun }) => {
			setFirstRun(firstRun)
		})
	}, [displayName])

	useEffect(() => {
		pb.collection("users")
			.listAuthMethods()
			.then((methods) => {
				setAuthMethods(methods)
			})
	}, [])

	const subtitle = useMemo(() => {
		if (isFirstRun) {
			return t`Please create an admin account`
		} else if (page?.route === "forgot_password") {
			return t`Enter email address to reset password`
		} else if (page?.route === "request_otp") {
			return t`Request a one-time password`
		} else {
			return t`Please sign in to your account`
		}
	}, [isFirstRun, page])

	useEffect(() => {
		return () => {
			if (shakeTimeoutRef.current !== null) window.clearTimeout(shakeTimeoutRef.current)
			if (flashTimeoutRef.current !== null) window.clearTimeout(flashTimeoutRef.current)
		}
	}, [])

	if (!authMethods) {
		return null
	}

	return (
		<div className="auth-screen">
			<div className={cn("auth-flash", flashMode !== "idle" && "is-active", flashMode === "success" && "is-success")} />
			<div className="auth-screen-noise" />
			<div className="auth-screen-orb auth-screen-orb-1" />
			<div className="auth-screen-orb auth-screen-orb-2" />
			<div className="auth-screen-orb auth-screen-orb-3" />
			<div className={cn("auth-card", isCardShaking && "auth-card-shake")}>
				<div className="auth-mode-toggle">
					<ModeToggle />
				</div>
				<div className="auth-card-header">
					<AuthEye isOpen={isEyeOpen} onOpen={openEye} />
					<div className="auth-copy text-center">
						<h1 className="auth-title">{displayName}</h1>
						<p className="auth-subtitle">{subtitle}</p>
					</div>
				</div>
				{page?.route === "forgot_password" ? (
					<ForgotPassword
						onSubmitStart={handleSubmitStart}
						onSubmitSuccess={handleSubmitSuccess}
						onSubmitError={handleSubmitError}
					/>
				) : page?.route === "request_otp" ? (
					<OtpRequestForm
						onSubmitStart={handleSubmitStart}
						onSubmitSuccess={handleSubmitSuccess}
						onSubmitError={handleSubmitError}
					/>
				) : (
					<UserAuthForm
						isFirstRun={isFirstRun}
						authMethods={authMethods}
						onFieldActivate={openEye}
						onSubmitStart={handleSubmitStart}
						onSubmitSuccess={handleSubmitSuccess}
						onSubmitError={handleSubmitError}
					/>
				)}
			</div>
		</div>
	)
}
