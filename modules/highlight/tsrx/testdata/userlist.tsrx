function UserList({ users, showBio }: Props) @{
  // one row per user
  @for (const user of users) {
    const initials = user.name.slice(0, 2).toUpperCase();
    const role = user.admin ? 'Admin' : 'Member';

    <div class="user-row">
      <span class="avatar">{initials}</span>
      <strong>{user.name}</strong>
      <span class="badge">{role}</span>

      @if (showBio && user.bio) {
        const short_bio = user.bio.slice(0, 140);
        const report = () => console.log('viewed ' + user.name);

        <div class="bio-detail">
          <p class="bio">{short_bio}</p>
          <button onClick={report}>Viewed</button>
        </div>
      }
    </div>
  }
}
